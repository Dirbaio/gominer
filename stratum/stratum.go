// Copyright (c) 2016-2023 The Decred developers.

package stratum

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/wire"
	"github.com/decred/go-socks/socks"
	"github.com/decred/gominer/util"
	"github.com/decred/gominer/work"
)

// ErrStratumStaleWork indicates that the work to send to the pool was stale.
var ErrStratumStaleWork = errors.New("stale work, throwing away")

// Stratum holds all the shared information for a stratum connection.
// XXX most of these should be unexported and use getters/setters.
type Stratum struct {
	// The following variables must only be used atomically.
	ValidShares   uint64
	InvalidShares uint64

	sync.Mutex
	cfg       Config
	Conn      net.Conn
	Reader    *bufio.Reader
	ID        uint64
	authID    uint64
	subID     uint64
	submitIDs []uint64
	Diff      float64
	Target    *big.Int
	PoolWork  NotifyWork

	Started uint32
}

// Config holdes the config options that may be used by a stratum pool.
type Config struct {
	Params    *chaincfg.Params
	Pool      string
	User      string
	Pass      string
	Proxy     string
	ProxyUser string
	ProxyPass string
	Version   string
}

// NotifyWork holds all the info received from a mining.notify message along
// with the Work data generate from it.
type NotifyWork struct {
	Clean             bool
	ExtraNonce1       string
	ExtraNonce2       uint64
	ExtraNonce2Length float64
	Nonce2            uint32
	CB1               string
	Height            int64
	NtimeDelta        int64
	JobID             string
	Hash              string
	Nbits             string
	Ntime             string
	Version           string
	NewWork           bool
	Work              *work.Work
}

// StratumMsg is the basic message object from stratum.
type StratumMsg struct {
	Method string `json:"method"`
	// Need to make generic.
	Params []string    `json:"params"`
	ID     interface{} `json:"id"`
}

// StratumRsp is the basic response type from stratum.
type StratumRsp struct {
	Method string `json:"method"`
	// Need to make generic.
	ID     interface{}      `json:"id"`
	Error  StratErr         `json:"error,omitempty"`
	Result *json.RawMessage `json:"result,omitempty"`
}

// StratErr is the basic error type (a number and a string) sent by
// the stratum server.
type StratErr struct {
	ErrNum uint64
	ErrStr string
	Result *json.RawMessage `json:"result,omitempty"`
}

// Basic reply is a reply type for any of the simple messages.
type BasicReply struct {
	ID     interface{} `json:"id"`
	Error  StratErr    `json:"error,omitempty"`
	Result bool        `json:"result"`
}

// SubscribeReply models the server response to a subscribe message.
type SubscribeReply struct {
	SubscribeID       string
	ExtraNonce1       string
	ExtraNonce2Length float64
}

// NotifyRes models the json from a mining.notify message.
type NotifyRes struct {
	JobID          string
	Hash           string
	GenTX1         string
	GenTX2         string
	MerkleBranches []string
	BlockVersion   string
	Nbits          string
	Ntime          string
	CleanJobs      bool
}

// Submit models a submission message.
type Submit struct {
	Params []string    `json:"params"`
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
}

// errJsonType is an error for json that we do not expect.
var errJsonType = errors.New("unexpected type in json")

func sliceContains(s []uint64, e uint64) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func sliceRemove(s []uint64, e uint64) []uint64 {
	for i, a := range s {
		if a == e {
			return append(s[:i], s[i+1:]...)
		}
	}

	return s
}

// StratumConn starts the initial connection to a stratum pool and sets defaults
// in the pool object.
func StratumConn(pool, user, pass, proxy, proxyUser, proxyPass, version string,
	chainParams *chaincfg.Params) (*Stratum, error) {

	var stratum Stratum
	stratum.cfg.Params = chainParams
	stratum.cfg.User = user
	stratum.cfg.Pass = pass
	stratum.cfg.Proxy = proxy
	stratum.cfg.ProxyUser = proxyUser
	stratum.cfg.ProxyPass = proxyPass
	stratum.cfg.Version = version

	log.Infof("Using pool: %v", pool)
	proto := "stratum+tcp://"
	if strings.HasPrefix(pool, proto) {
		pool = strings.Replace(pool, proto, "", 1)
	} else {
		err := errors.New("only stratum pools supported")
		return nil, err
	}
	var conn net.Conn
	var err error
	if stratum.cfg.Proxy != "" {
		proxy := &socks.Proxy{
			Addr:     stratum.cfg.Proxy,
			Username: stratum.cfg.ProxyUser,
			Password: stratum.cfg.ProxyPass,
		}
		conn, err = proxy.Dial("tcp", pool)
	} else {
		conn, err = net.Dial("tcp", pool)
	}
	if err != nil {
		return nil, err
	}
	stratum.ID = 1
	stratum.Conn = conn
	stratum.cfg.Pool = pool

	// We will set it for sure later but this really should be the value and
	// setting it here will prevent so incorrect matches based on the
	// default 0 value.
	stratum.authID = 2

	// Target for share is 1 unless we hear otherwise.
	stratum.Diff = 1
	stratum.Target, err = util.DiffToTarget(stratum.Diff, chainParams.PowLimit)
	if err != nil {
		return nil, err
	}
	stratum.PoolWork.NewWork = false
	stratum.Reader = bufio.NewReader(stratum.Conn)
	go stratum.Listen()

	err = stratum.Subscribe()
	if err != nil {
		return nil, err
	}
	err = stratum.Auth()
	if err != nil {
		return nil, err
	}

	stratum.Started = uint32(time.Now().Unix())

	return &stratum, nil
}

// Reconnect reconnects to a stratum server if the connection has been lost.
func (s *Stratum) Reconnect() error {
	var conn net.Conn
	var err error
	if s.cfg.Proxy != "" {
		proxy := &socks.Proxy{
			Addr:     s.cfg.Proxy,
			Username: s.cfg.ProxyUser,
			Password: s.cfg.ProxyPass,
		}
		conn, err = proxy.Dial("tcp", s.cfg.Pool)
	} else {
		conn, err = net.Dial("tcp", s.cfg.Pool)
	}
	if err != nil {
		return err
	}
	s.Conn = conn
	s.Reader = bufio.NewReader(s.Conn)
	err = s.Subscribe()
	if err != nil {
		return nil
	}
	// Should NOT need this.
	time.Sleep(5 * time.Second)
	// XXX Do I really need to re-auth here?
	err = s.Auth()
	if err != nil {
		return nil
	}

	// If we were able to reconnect, restart counter
	s.Started = uint32(time.Now().Unix())

	return nil
}

// Listen is the listener for the incoming messages from the stratum pool.
func (s *Stratum) Listen() {
	log.Debug("Starting Listener")

	for {
		result, err := s.Reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Error("Connection lost!  Reconnecting.")
				err = s.Reconnect()
				if err != nil {
					log.Error(err)
					log.Error("Reconnect failed.")
					os.Exit(1)
					return
				}

			} else {
				log.Error(err)
			}
			continue
		}

		log.Debug(strings.TrimSuffix(result, "\n"))
		resp, err := s.Unmarshal([]byte(result))
		if err != nil {
			log.Error(err)
			continue
		}

		switch resp.(type) {
		case *BasicReply:
			s.handleBasicReply(resp)
		case StratumMsg:
			s.handleStratumMsg(resp)
		case NotifyRes:
			s.handleNotifyRes(resp)
		case *SubscribeReply:
			s.handleSubscribeReply(resp)
		default:
			log.Info("Unhandled message: ", result)
		}
	}
}

func (s *Stratum) handleBasicReply(resp interface{}) {
	s.Lock()
	defer s.Unlock()
	aResp := resp.(*BasicReply)

	if int(aResp.ID.(uint64)) == int(s.authID) {
		if aResp.Result {
			log.Debug("Logged in")
		} else {
			log.Error("Auth failure.")
		}
	}
	if sliceContains(s.submitIDs, aResp.ID.(uint64)) {
		if aResp.Result {
			atomic.AddUint64(&s.ValidShares, 1)
			log.Debug("Share accepted")
		} else {
			atomic.AddUint64(&s.InvalidShares, 1)
			log.Error("Share rejected: ", aResp.Error.ErrStr)
		}
		s.submitIDs = sliceRemove(s.submitIDs, aResp.ID.(uint64))
	}
}

func (s *Stratum) handleStratumMsg(resp interface{}) {
	nResp := resp.(StratumMsg)
	log.Trace(nResp)
	// Too much is still handled in unmarshaler.  Need to
	// move stuff other than unmarshalling here.
	switch nResp.Method {
	case "client.show_message":
		log.Info(nResp.Params)
	case "client.reconnect":
		log.Debug("Reconnect requested")
		wait, err := strconv.Atoi(nResp.Params[2])
		if err != nil {
			log.Error(err)
			return
		}
		time.Sleep(time.Duration(wait) * time.Second)
		pool := nResp.Params[0] + ":" + nResp.Params[1]
		s.cfg.Pool = pool
		err = s.Reconnect()
		if err != nil {
			log.Error(err)
			// XXX should just die at this point
			// but we don't really have access to
			// the channel to end everything.
			return
		}

	case "client.get_version":
		log.Debug("get_version request received.")
		msg := StratumMsg{
			Method: nResp.Method,
			ID:     nResp.ID,
			Params: []string{"decred-gominer/" + s.cfg.Version},
		}
		m, err := json.Marshal(msg)
		if err != nil {
			log.Error(err)
			return
		}
		_, err = s.Conn.Write(m)
		if err != nil {
			log.Error(err)
			return
		}
		_, err = s.Conn.Write([]byte("\n"))
		if err != nil {
			log.Error(err)
			return
		}
	}
}

func (s *Stratum) handleNotifyRes(resp interface{}) {
	s.Lock()
	defer s.Unlock()
	nResp := resp.(NotifyRes)
	s.PoolWork.JobID = nResp.JobID
	s.PoolWork.CB1 = nResp.GenTX1
	heightHex := nResp.GenTX1[186:188] + nResp.GenTX1[184:186]
	height, err := strconv.ParseInt(heightHex, 16, 32)
	if err != nil {
		log.Debugf("failed to parse height %v", err)
		height = 0
	}

	s.PoolWork.Height = height
	s.PoolWork.Hash = nResp.Hash
	s.PoolWork.Nbits = nResp.Nbits
	s.PoolWork.Version = nResp.BlockVersion
	parsedNtime, err := strconv.ParseInt(nResp.Ntime, 16, 64)
	if err != nil {
		log.Error(err)
	}

	s.PoolWork.Ntime = nResp.Ntime
	s.PoolWork.NtimeDelta = parsedNtime - time.Now().Unix()
	s.PoolWork.Clean = nResp.CleanJobs
	s.PoolWork.NewWork = true
	log.Trace("notify: ", spew.Sdump(nResp))
}

func (s *Stratum) handleSubscribeReply(resp interface{}) {
	nResp := resp.(*SubscribeReply)
	s.PoolWork.ExtraNonce1 = nResp.ExtraNonce1
	s.PoolWork.ExtraNonce2Length = nResp.ExtraNonce2Length
	log.Debug("Subscribe reply received.")
	log.Trace(spew.Sdump(resp))
}

// Auth sends a message to the pool to authorize a worker.
func (s *Stratum) Auth() error {
	msg := StratumMsg{
		Method: "mining.authorize",
		ID:     s.ID,
		Params: []string{s.cfg.User, s.cfg.Pass},
	}
	// Auth reply has no method so need a way to identify it.
	// Ugly, but not much choice.
	id, ok := msg.ID.(uint64)
	if !ok {
		return errJsonType
	}
	s.authID = id
	s.ID++
	log.Tracef("%v", msg)
	m, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = s.Conn.Write(m)
	if err != nil {
		return err
	}
	_, err = s.Conn.Write([]byte("\n"))
	if err != nil {
		return err
	}
	return nil
}

// Subscribe sends the subscribe message to get mining info for a worker.
func (s *Stratum) Subscribe() error {
	msg := StratumMsg{
		Method: "mining.subscribe",
		ID:     s.ID,
		Params: []string{"decred-gominer/" + s.cfg.Version},
	}
	s.subID = msg.ID.(uint64)
	s.ID++
	m, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	log.Tracef("%v", string(m))
	_, err = s.Conn.Write(m)
	if err != nil {
		return err
	}
	_, err = s.Conn.Write([]byte("\n"))
	if err != nil {
		return err
	}
	return nil
}

// Unmarshal provides a json unmarshaler for the commands.
// I'm sure a lot of this can be generalized but the json we deal with
// is pretty yucky.
func (s *Stratum) Unmarshal(blob []byte) (interface{}, error) {
	s.Lock()
	defer s.Unlock()
	var (
		objmap map[string]json.RawMessage
		method string
		id     uint64
	)

	err := json.Unmarshal(blob, &objmap)
	if err != nil {
		return nil, err
	}
	// decode command
	// Not everyone has a method.
	err = json.Unmarshal(objmap["method"], &method)
	if err != nil {
		method = ""
	}
	err = json.Unmarshal(objmap["id"], &id)
	if err != nil {
		return nil, err
	}
	log.Trace("Received: method: ", method, " id: ", id)
	if id == s.authID {
		var (
			objmap      map[string]json.RawMessage
			id          uint64
			result      bool
			errorHolder []interface{}
		)
		err := json.Unmarshal(blob, &objmap)
		if err != nil {
			return nil, err
		}
		resp := &BasicReply{}

		err = json.Unmarshal(objmap["id"], &id)
		if err != nil {
			return nil, err
		}
		resp.ID = id

		err = json.Unmarshal(objmap["result"], &result)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(objmap["error"], &errorHolder)
		if err != nil {
			return nil, err
		}
		resp.Result = result

		if errorHolder != nil {
			errN, ok := errorHolder[0].(float64)
			if !ok {
				return nil, errJsonType
			}
			errS, ok := errorHolder[1].(string)
			if !ok {
				return nil, errJsonType
			}
			resp.Error.ErrNum = uint64(errN)
			resp.Error.ErrStr = errS
		}

		return resp, nil

	}
	if id == s.subID {
		var resi []interface{}
		err := json.Unmarshal(objmap["result"], &resi)
		if err != nil {
			return nil, err
		}
		log.Trace(resi)
		resp := &SubscribeReply{}

		var objmap2 map[string]json.RawMessage
		err = json.Unmarshal(blob, &objmap2)
		if err != nil {
			return nil, err
		}

		var resJS []json.RawMessage
		err = json.Unmarshal(objmap["result"], &resJS)
		if err != nil {
			return nil, err
		}

		if len(resJS) == 0 {
			return nil, errJsonType
		}

		var msgPeak []interface{}
		err = json.Unmarshal(resJS[0], &msgPeak)
		if err != nil {
			return nil, err
		}

		// The pools do not all agree on what this message looks like
		// so we need to actually look at it before unmarshalling for
		// real so we can use the right form.  Yuck.
		if msgPeak[0] == "mining.notify" {
			var innerMsg []string
			err = json.Unmarshal(resJS[0], &innerMsg)
			if err != nil {
				return nil, err
			}
			resp.SubscribeID = innerMsg[1]
		} else {
			var innerMsg [][]string
			err = json.Unmarshal(resJS[0], &innerMsg)
			if err != nil {
				return nil, err
			}

			for i := 0; i < len(innerMsg); i++ {
				if innerMsg[i][0] == "mining.notify" {
					resp.SubscribeID = innerMsg[i][1]
				}
				if innerMsg[i][0] == "mining.set_difficulty" {
					// Not all pools correctly put something
					// in here so we will ignore it (we
					// already have the default value of 1
					// anyway and pool can send a new one.
					// dcr.coinmine.pl puts something that
					// is not a difficulty here which is why
					// we ignore.
				}
			}
		}

		resp.ExtraNonce1 = resi[1].(string)
		resp.ExtraNonce2Length = resi[2].(float64)
		return resp, nil
	}
	if sliceContains(s.submitIDs, id) {
		var (
			objmap      map[string]json.RawMessage
			id          uint64
			result      bool
			errorHolder []interface{}
		)
		err := json.Unmarshal(blob, &objmap)
		if err != nil {
			return nil, err
		}
		resp := &BasicReply{}

		err = json.Unmarshal(objmap["id"], &id)
		if err != nil {
			return nil, err
		}
		resp.ID = id

		err = json.Unmarshal(objmap["result"], &result)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(objmap["error"], &errorHolder)
		if err != nil {
			return nil, err
		}
		resp.Result = result

		if errorHolder != nil {
			errN, ok := errorHolder[0].(float64)
			if !ok {
				return nil, errJsonType
			}
			errS, ok := errorHolder[1].(string)
			if !ok {
				return nil, errJsonType
			}
			resp.Error.ErrNum = uint64(errN)
			resp.Error.ErrStr = errS
		}

		return resp, nil
	}
	switch method {
	case "mining.notify":
		log.Trace("Unmarshal mining.notify")
		var resi []interface{}
		err := json.Unmarshal(objmap["params"], &resi)
		if err != nil {
			return nil, err
		}
		var nres = NotifyRes{}
		jobID, ok := resi[0].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.JobID = jobID
		hash, ok := resi[1].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.Hash = hash
		genTX1, ok := resi[2].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.GenTX1 = genTX1
		// Ignore the GenTX2 (param 3) and Merkle Branches (param 4) fields
		// since they do not apply to Decred.
		blockVersion, ok := resi[5].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.BlockVersion = blockVersion
		nbits, ok := resi[6].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.Nbits = nbits
		ntime, ok := resi[7].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.Ntime = ntime
		cleanJobs, ok := resi[8].(bool)
		if !ok {
			return nil, errJsonType
		}
		nres.CleanJobs = cleanJobs
		return nres, nil

	case "mining.set_difficulty":
		log.Trace("Received new difficulty.")
		var resi []interface{}
		err := json.Unmarshal(objmap["params"], &resi)
		if err != nil {
			return nil, err
		}

		difficulty, ok := resi[0].(float64)
		if !ok {
			return nil, errJsonType
		}
		s.Target, err = util.DiffToTarget(difficulty, s.cfg.Params.PowLimit)
		if err != nil {
			return nil, err
		}
		s.Diff = difficulty
		var nres = StratumMsg{}
		nres.Method = method
		diffStr := strconv.FormatFloat(difficulty, 'E', -1, 32)
		var params []string
		params = append(params, diffStr)
		nres.Params = params
		log.Infof("Stratum difficulty set to %v", difficulty)
		return nres, nil

	case "client.show_message":
		var resi []interface{}
		err := json.Unmarshal(objmap["result"], &resi)
		if err != nil {
			return nil, err
		}
		msg, ok := resi[0].(string)
		if !ok {
			return nil, errJsonType
		}
		var nres = StratumMsg{}
		nres.Method = method
		var params []string
		params = append(params, msg)
		nres.Params = params
		return nres, nil

	case "client.get_version":
		var nres = StratumMsg{}
		var id uint64
		err = json.Unmarshal(objmap["id"], &id)
		if err != nil {
			return nil, err
		}
		nres.Method = method
		nres.ID = id
		return nres, nil

	case "client.reconnect":
		var nres = StratumMsg{}
		var id uint64
		err = json.Unmarshal(objmap["id"], &id)
		if err != nil {
			return nil, err
		}
		nres.Method = method
		nres.ID = id

		var resi []interface{}
		err := json.Unmarshal(objmap["params"], &resi)
		if err != nil {
			return nil, err
		}
		log.Trace(resi)

		if len(resi) < 3 {
			return nil, errJsonType
		}
		hostname, ok := resi[0].(string)
		if !ok {
			return nil, errJsonType
		}
		p, ok := resi[1].(float64)
		if !ok {
			return nil, errJsonType
		}
		port := strconv.Itoa(int(p))
		w, ok := resi[2].(float64)
		if !ok {
			return nil, errJsonType
		}
		wait := strconv.Itoa(int(w))

		nres.Params = []string{hostname, port, wait}

		return nres, nil

	default:
		resp := &StratumRsp{}
		err := json.Unmarshal(blob, &resp)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}
}

// PrepWork converts the stratum notify to getwork style data for mining.
func (s *Stratum) PrepWork() error {
	// Decode the previous block hash.  Stratum should provide the hash in
	// internal byte order (meaning the exact order produced by the hash
	// function).
	//
	// Note that this is reversed from how it typically appears when presented
	// to humans in places such as block explorers which treat them as little
	// endian uint256s.
	prevHashBytes, err := hex.DecodeString(s.PoolWork.Hash)
	if err != nil {
		return fmt.Errorf("error decoding previous block hash: %w", err)
	}

	// Decode extranonce1 which is basically the pool user and worker ID.
	const maxExtraNonce1Len = 4
	en1, err := hex.DecodeString(s.PoolWork.ExtraNonce1)
	if err != nil {
		return fmt.Errorf("error decoding ExtraNonce1 field: %w", err)
	}
	if len(en1) > maxExtraNonce1Len {
		return fmt.Errorf("extraNonce1 length must be a max of %d bytes",
			maxExtraNonce1Len)
	}

	// Require a minimum of 4 bytes and a maximum of 12 bytes for the length of
	// extraNonce2.
	const requiredExtraNonce2Len = 4
	const maxExtraNonce2Len = 12
	if s.PoolWork.ExtraNonce2Length < requiredExtraNonce2Len {
		return fmt.Errorf("extraNonce2 length must be at least %d bytes",
			requiredExtraNonce2Len)
	}
	if s.PoolWork.ExtraNonce2Length > maxExtraNonce2Len {
		return fmt.Errorf("extraNonce2 length must be a max of %d bytes",
			maxExtraNonce2Len)
	}

	// The stratum "protocol" (which is not actually very well defined) does not
	// individually provide all of the information the Decred header needs nor
	// does it provide an official way to extend it.
	//
	// Further, the Decred header explicitly provides additional space which
	// removes the need to create a new coinbase and update the merkle root.
	//
	// In order to address these things, the field that was intended to serve
	// for the coinbase (generate transaction) is instead repurposed to contain
	// the serialized partial header for everything after the previous block
	// hash in the format it is to be hashed.
	partialHeader, err := hex.DecodeString(s.PoolWork.CB1)
	if err != nil {
		return fmt.Errorf("error decoding cb1 field (partial header): %w", err)
	}

	// Decode block version.
	//
	// The block version must be in little endian in the serialized header and
	// stratum should provide all fields in little endian.  Thus, no conversion
	// is needed.
	blockVerBytes, err := hex.DecodeString(s.PoolWork.Version)
	if err != nil {
		return fmt.Errorf("error decoding version field: %w", err)
	}

	// Decode timestamp.
	//
	// Stratum should provide all fields in little endian.
	timestampBytes, err := hex.DecodeString(s.PoolWork.Ntime)
	if err != nil {
		return fmt.Errorf("error decoding timestamp field: %w", err)
	}
	timestamp := binary.LittleEndian.Uint32(timestampBytes)

	// Assemble work with provided details.
	//
	// The getwork data format consists of the serialized block header followed
	// by the additional blake3 padding needed to bring the data length to a
	// multiple of the blake3 block size.  Since the blake3 block size is 64
	// bytes and the header is 180 bytes, the next multiple is 192 bytes.
	//
	// See the comment on the partial header above for the rationale here.
	//
	// Note that the timestamp is not set in the work data here because it is
	// passed along to the mining process separately where it is updated and set
	// accordingly as work is performed.  It is also worth noting that the pool
	// providing the work should typically have already set the provided
	// timestamp in the provided partial header too, but this implementation
	// does not rely on that assumption.
	var workData [192]byte
	offset := 0
	offset += copy(workData[offset:], blockVerBytes)
	offset += copy(workData[offset:], prevHashBytes)
	copy(workData[offset:], partialHeader)

	// Set the provided extra nonce at the expected offset.  The protocol
	// expects it to be in the serialized header just after the 4-byte nonce.
	copy(workData[144:], en1)

	if s.Target == nil {
		log.Errorf("No target set!  Reconnecting to pool.")
		err = s.Reconnect()
		if err != nil {
			log.Error(err)
			// XXX should just die at this point
			// but we don't really have access to
			// the channel to end everything.
			return err
		}
		return nil
	}

	const isGetWork = false
	receivedTime := uint32(time.Now().Unix())
	w := work.NewWork(workData, s.Target, timestamp, receivedTime, isGetWork)
	s.PoolWork.Work = w
	log.Tracef("Stratum prepared work data %x, target %064x", w.Data[:],
		w.Target)

	return nil
}

// PrepSubmit formats a mining.sumbit message from the solved work.
func (s *Stratum) PrepSubmit(data []byte) (Submit, error) {
	headerBytes := data[0:wire.MaxBlockHeaderPayload]
	log.Debugf("Stratum got valid work to submit %x (block hash %v)", data,
		chainhash.Hash(blake256.Sum256(headerBytes)))

	sub := Submit{}
	sub.Method = "mining.submit"

	var submittedHeader wire.BlockHeader
	err := submittedHeader.Deserialize(bytes.NewReader(headerBytes))
	if err != nil {
		log.Error("Error generating header")
		return sub, err
	}

	s.ID++
	sub.ID = s.ID
	s.submitIDs = append(s.submitIDs, s.ID)

	// The fields in the serialized header must be in little endian and stratum
	// fields for numeric values should be in little endian.  Thus, no
	// endianness conversion is needed.
	uint32LEHex := func(startOffset int) string {
		return hex.EncodeToString(data[startOffset : startOffset+4])
	}
	timestampHex := uint32LEHex(128 + 4*work.TimestampWord)
	nonceHex := uint32LEHex(128 + 4*work.Nonce0Word)
	extraNonce2Hex := uint32LEHex(128 + 4*work.Nonce2Word)
	sub.Params = []string{s.cfg.User, s.PoolWork.JobID, extraNonce2Hex,
		timestampHex, nonceHex}

	return sub, nil
}
