// Copyright (c) 2016 The Decred developers

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
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
	"time"

	"github.com/davecgh/go-spew/spew"

	"github.com/decred/dcrd/wire"
)

// Stratum holds all the shared information for a stratum connection.
// XXX most of these should be unexported and use getters/setters.
type Stratum struct {
	Pool      string
	User      string
	Pass      string
	Conn      net.Conn
	Reader    *bufio.Reader
	ID        uint64
	authID    uint64
	subID     uint64
	submitID  uint64
	Diff      float64
	Target    string
	submitted bool
	PoolWork  NotifyWork
}

// NotifyWork holds all the info recieved from a mining.notify message along
// with the Work data generate from it.
type NotifyWork struct {
	Clean             bool
	ExtraNonce1       string
	ExtraNonce2       uint64
	ExtraNonce2Length float64
	CB1               string
	CB2               string
	Height            int64
	NtimeDelta        int64
	JobID             string
	Hash              string
	Nbits             string
	Ntime             string
	Version           string
	NewWork           bool
	Work              *Work
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
	Method string      `json:"method"`
	Params []string    `json:"params"`
	ID     interface{} `json:"id"`
}

// errJsonType is an error for json that we do not expect.
var errJsonType = errors.New("Unexpected type in json.")

// StratumConn starts the initial connection to a stratum pool and sets defaults
// in the pool object.
func StratumConn(pool, user, pass string) (*Stratum, error) {
	poolLog.Infof("Using pool: %v", pool)
	proto := "stratum+tcp://"
	if strings.HasPrefix(pool, proto) {
		pool = strings.Replace(pool, proto, "", 1)
	} else {
		err := errors.New("Only stratum pools supported.")
		return nil, err
	}
	conn, err := net.Dial("tcp", pool)
	if err != nil {
		return nil, err
	}
	var stratum Stratum
	stratum.ID = 1
	stratum.Conn = conn
	stratum.Pool = pool
	stratum.User = user
	stratum.Pass = pass
	// We will set it for sure later but this really should be the value and
	// setting it here will prevent so incorrect matches based on the
	// default 0 value.
	stratum.authID = 2
	// Target for share is 1 unless we hear otherwise.
	stratum.Diff = 1
	stratum.Target = stratum.diffToTarget(stratum.Diff)
	stratum.PoolWork.NewWork = false
	stratum.Reader = bufio.NewReader(stratum.Conn)
	go stratum.Listen()

	err = stratum.Subscribe()
	if err != nil {
		return nil, err
	}
	// Should NOT need this.
	//time.Sleep(5 * time.Second)
	err = stratum.Auth()
	if err != nil {
		return nil, err
	}

	return &stratum, nil
}

// Reconnect reconnects to a stratum server if the connection has been lost.
func (s *Stratum) Reconnect() error {
	conn, err := net.Dial("tcp", s.Pool)
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
	return nil
}

// Listen is the listener for the incoming messages from the stratum pool.
func (s *Stratum) Listen() {
	poolLog.Debug("Starting Listener")

	for {
		result, err := s.Reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				poolLog.Error("Connection lost!  Reconnecting.")
				err = s.Reconnect()
				if err != nil {
					poolLog.Error(err)
					poolLog.Error("Reconnect failed.")
					os.Exit(1)
					return
				}

			} else {
				poolLog.Error(err)
			}
			continue
		}
		poolLog.Debug(strings.TrimSuffix(result, "\n"))
		resp, err := s.Unmarshal([]byte(result))
		if err != nil {
			poolLog.Error(err)
			continue
		}
		switch resp.(type) {
		case *BasicReply:
			aResp := resp.(*BasicReply)
			if int(aResp.ID.(uint64)) == int(s.authID) {
				if aResp.Result {
					poolLog.Info("Logged in")
				} else {
					poolLog.Error("Auth failure.")
				}
			}
			if aResp.ID == s.submitID {
				if aResp.Result {
					poolLog.Info("Share Accepted")
				} else {
					poolLog.Error("Share rejected: ", aResp.Error.ErrStr)
				}
				s.submitted = false
			}
		case StratumMsg:
			nResp := resp.(StratumMsg)
			poolLog.Trace(nResp)
			// Too much is still handled in unmarshaler.  Need to
			// move stuff other than unmarshalling here.
			switch nResp.Method {
			case "client.show_message":
				poolLog.Info(nResp.Params)
			case "client.reconnect":
				poolLog.Info("Reconnect requested")
				wait, err := strconv.Atoi(nResp.Params[2])
				if err != nil {
					poolLog.Error(err)
					continue
				}
				time.Sleep(time.Duration(wait) * time.Second)
				pool := nResp.Params[0] + ":" + nResp.Params[1]
				s.Pool = pool
				err = s.Reconnect()
				if err != nil {
					poolLog.Error(err)
					// XXX should just die at this point
					// but we don't really have access to
					// the channel to end everything.
					return
				}
			case "client.get_version":
				poolLog.Debug("get_version request received.")
				msg := StratumMsg{
					Method: nResp.Method,
					ID:     nResp.ID,
					Params: []string{"decred-gominer/" + version()},
				}
				m, err := json.Marshal(msg)
				if err != nil {
					poolLog.Error(err)
					continue
				}
				_, err = s.Conn.Write(m)
				if err != nil {
					poolLog.Error(err)
					continue
				}
				_, err = s.Conn.Write([]byte("\n"))
				if err != nil {
					poolLog.Error(err)
					continue
				}
			}
		case NotifyRes:
			nResp := resp.(NotifyRes)
			s.PoolWork.JobID = nResp.JobID
			s.PoolWork.CB1 = nResp.GenTX1
			//poolLog.Trace("CB1: " + spew.Sdump(s.PoolWork.CB1))
			//height := nResp.GenTX1[184:188]
			heightHex := nResp.GenTX1[186:188] + nResp.GenTX1[184:186]
			height, err := strconv.ParseInt(heightHex, 16, 32)
			if err != nil {
				poolLog.Tracef("failed to parse height %v", err)
				height = 0
			}
			s.PoolWork.Height = height
			s.PoolWork.CB2 = nResp.GenTX2
			s.PoolWork.Hash = nResp.Hash
			s.PoolWork.Nbits = nResp.Nbits
			s.PoolWork.Version = nResp.BlockVersion
			parsedNtime, err := strconv.ParseInt(nResp.Ntime, 16, 64)
			if err != nil {
				poolLog.Error(err)
			}
			s.PoolWork.Ntime = nResp.Ntime
			s.PoolWork.NtimeDelta = parsedNtime - time.Now().Unix()
			s.PoolWork.Clean = nResp.CleanJobs
			s.PoolWork.NewWork = true
			poolLog.Trace("notify: ", spew.Sdump(nResp))
		case *SubscribeReply:
			nResp := resp.(*SubscribeReply)
			s.PoolWork.ExtraNonce1 = nResp.ExtraNonce1
			s.PoolWork.ExtraNonce2Length = nResp.ExtraNonce2Length
			poolLog.Info("Subscribe reply received.")
			poolLog.Trace(spew.Sdump(resp))
		default:
			poolLog.Info("Unhandled message: ", result)
		}
	}
}

// Auth sends a message to the pool to authorize a worker.
func (s *Stratum) Auth() error {
	msg := StratumMsg{
		Method: "mining.authorize",
		ID:     s.ID,
		Params: []string{s.User, s.Pass},
	}
	// Auth reply has no method so need a way to identify it.
	// Ugly, but not much choise.
	id, ok := msg.ID.(uint64)
	if !ok {
		return errJsonType
	}
	s.authID = id
	s.ID += 1
	poolLog.Tracef("> %v", msg)
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
		Params: []string{"decred-gominer/" + version()},
	}
	s.subID = msg.ID.(uint64)
	s.ID++
	m, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	poolLog.Tracef("> %v", string(m))
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

// Unmarshal provides a json umnarshaler for the commands.
// I'm sure a lot of this can be generalized but the json we deal with
// is pretty yucky.
func (s *Stratum) Unmarshal(blob []byte) (interface{}, error) {
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
	// Not everyone has a method
	err = json.Unmarshal(objmap["method"], &method)
	if err != nil {
		method = ""
	}
	err = json.Unmarshal(objmap["id"], &id)
	if err != nil {
		return nil, err
	}
	poolLog.Trace("Received: method: ", method, " id: ", id)
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
		poolLog.Trace(resi)
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
	if id == s.submitID && s.submitted {
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
		poolLog.Trace("Unmarshal mining.notify")
		var resi []interface{}
		err := json.Unmarshal(objmap["params"], &resi)
		if err != nil {
			return nil, err
		}
		poolLog.Trace(resi)
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
		genTX2, ok := resi[3].(string)
		if !ok {
			return nil, errJsonType
		}
		nres.GenTX2 = genTX2
		//ccminer code also confirms this
		//nres.MerkleBranches = resi[4].([]string)
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
		poolLog.Trace("Received new difficulty.")
		var resi []interface{}
		err := json.Unmarshal(objmap["params"], &resi)
		if err != nil {
			return nil, err
		}

		difficulty, ok := resi[0].(float64)
		if !ok {
			return nil, errJsonType
		}
		s.Target = s.diffToTarget(difficulty)
		s.Diff = difficulty
		var nres = StratumMsg{}
		nres.Method = method
		diffStr := strconv.FormatFloat(difficulty, 'E', -1, 32)
		var params []string
		params = append(params, diffStr)
		nres.Params = params
		poolLog.Infof("Stratum difficulty set to %v", difficulty)
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
		poolLog.Trace(resi)

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

	// Build final extranonce
	en1, err := hex.DecodeString(s.PoolWork.ExtraNonce1)
	if err != nil {
		poolLog.Error("Error decoding ExtraNonce1.")
		return err
	}
	poolLog.Debugf("en1 %v s.PoolWork.ExtraNonce1 %v", en1, s.PoolWork.ExtraNonce1)
	// Work out padding
	tmp := []string{"%0", strconv.Itoa(int(s.PoolWork.ExtraNonce2Length) * 2), "x"}
	fmtString := strings.Join(tmp, "")
	en2, err := hex.DecodeString(fmt.Sprintf(fmtString, s.PoolWork.ExtraNonce2))
	if err != nil {
		poolLog.Error("Error decoding ExtraNonce2.")
		return err
	}
	poolLog.Debugf("en2 %v s.PoolWork.ExtraNonce2 %v", en2, s.PoolWork.ExtraNonce2)
	extraNonce := append(en1[:], en2[:]...)
	poolLog.Debugf("extraNonce %v", extraNonce)

	// Increase extranonce2
	s.PoolWork.ExtraNonce2++

	// Put coinbase transaction together

	cb1, err := hex.DecodeString(s.PoolWork.CB1)
	if err != nil {
		poolLog.Error("Error decoding Coinbase pt 1.")
		return err
	}
	poolLog.Debugf("cb1 %v s.PoolWork.CB1 %v", cb1, s.PoolWork.CB1)

	// I've never actually seen a cb2.
	cb2, err := hex.DecodeString(s.PoolWork.CB2)
	if err != nil {
		poolLog.Error("Error decoding Coinbase pt 2.")
		return err
	}
	poolLog.Debugf("cb2 %v s.PoolWork.CB2 %v", cb2, s.PoolWork.CB2)

	cb := append(cb1[:], extraNonce[:]...)
	cb = append(cb[:], cb2[:]...)
	poolLog.Debugf("cb %v", cb)

	// Calculate merkle root
	// I have never seen anything sent in the merkle tree
	// sent by the pool so not much I can do here.
	// Confirmed in ccminer code.
	// Same for StakeRoot

	// Generate current ntime
	ntime := time.Now().Unix() + s.PoolWork.NtimeDelta

	poolLog.Tracef("ntime: %v", ntime)

	// Serialize header
	bh := wire.BlockHeader{}
	v, err := reverseToInt(s.PoolWork.Version)
	if err != nil {
		return err
	}
	bh.Version = v

	nbits, err := hex.DecodeString(s.PoolWork.Nbits)
	if err != nil {
		poolLog.Error("Error decoding nbits")
		return err
	}

	b, _ := binary.Uvarint(nbits)
	bh.Bits = uint32(b)
	t := time.Now().Unix() + s.PoolWork.NtimeDelta
	bh.Timestamp = time.Unix(t, 0)
	bh.Nonce = 0
	// Serialized version
	blockHeader, err := bh.Bytes()
	if err != nil {
		return err
	}

	target, err := hex.DecodeString(s.Target)
	if err != nil {
		poolLog.Error("Error decoding Target")
		return err
	}
	if len(target) != 32 {
		return fmt.Errorf("Wrong target length: got %d, expected 32", len(target))
	}

	data := blockHeader
	poolLog.Debugf("data0 %v", data)
	poolLog.Tracef("data len %v", len(data))
	copy(data[31:139], cb1[0:108])
	poolLog.Debugf("data1 %v", data)

	var workdata [180]byte
	workPosition := 0

	version := new(bytes.Buffer)
	err = binary.Write(version, binary.LittleEndian, v)
	if err != nil {
		return err
	}
	copy(workdata[workPosition:], version.Bytes())
	poolLog.Debugf("appended version.Bytes() %v", version.Bytes())
	poolLog.Tracef("partial workdata (version): %v", hex.EncodeToString(workdata[:]))

	prevHash := revHash(s.PoolWork.Hash)
	p, err := hex.DecodeString(prevHash)
	if err != nil {
		poolLog.Error("Error encoding previous hash.")
		return err
	}

	workPosition += 4
	copy(workdata[workPosition:], p)
	poolLog.Tracef("partial workdata (previous hash): %v", hex.EncodeToString(workdata[:]))
	poolLog.Debugf("prevHash %v", prevHash)

	workPosition += 32
	copy(workdata[workPosition:], cb1[0:108])
	poolLog.Tracef("partial workdata (cb1): %v", hex.EncodeToString(workdata[:]))

	workPosition += 108
	copy(workdata[workPosition:], extraNonce)
	poolLog.Debugf("extranonce: %v", hex.EncodeToString(extraNonce))
	poolLog.Tracef("partial workdata (extranonce): %v", hex.EncodeToString(workdata[:]))

	var randomBytes = make([]byte, 4)
	_, err = rand.Read(randomBytes)
	if err != nil {
		poolLog.Errorf("Unable to generate random bytes")
	}
	workPosition += 4
	copy(workdata[workPosition:], randomBytes)

	poolLog.Debugf("workdata len %v", len(workdata))
	poolLog.Tracef("workdata %v", hex.EncodeToString(workdata[:]))

	var w Work
	copy(w.Data[:], workdata[:])
	copy(w.Target[:], target)
	poolLog.Tracef("final data %v, target %v", hex.EncodeToString(data), hex.EncodeToString(target))
	s.PoolWork.Work = &w
	return nil

}

// PrepSubmit formats a mining.sumbit message from the solved work.
func (s *Stratum) PrepSubmit(data []byte) (Submit, error) {
	sub := Submit{}
	sub.Method = "mining.submit"

	// Format data to send off.

	hexData := hex.EncodeToString(data)
	decodedData, err := hex.DecodeString(hexData)
	if err != nil {
		poolLog.Error("Error decoding data.")
		return sub, err
	}

	var submittedHeader wire.BlockHeader
	bhBuf := bytes.NewReader(decodedData[0:wire.MaxBlockHeaderPayload])
	err = submittedHeader.Deserialize(bhBuf)
	if err != nil {
		poolLog.Error("Error generating header.")
		return sub, err
	}

	//en2 := strconv.FormatUint(s.PoolWork.ExtraNonce2, 16)
	nonce := strconv.FormatUint(uint64(submittedHeader.Nonce), 16)
	time := encodeTime(submittedHeader.Timestamp)

	en1, err := hex.DecodeString(s.PoolWork.ExtraNonce1)
	if err != nil {
		poolLog.Error("Error decoding ExtraNonce1.")
		//return err
	}
	poolLog.Tracef("en1 %v s.PoolWork.ExtraNonce1 %v", en1, s.PoolWork.ExtraNonce1)
	// Work out padding
	tmp := []string{"%0", strconv.Itoa(int(s.PoolWork.ExtraNonce2Length) * 2), "x"}
	fmtString := strings.Join(tmp, "")
	en2, err := hex.DecodeString(fmt.Sprintf(fmtString, s.PoolWork.ExtraNonce2))
	if err != nil {
		poolLog.Error("Error decoding ExtraNonce2.")
		//return err
	}
	poolLog.Tracef("en2 %v s.PoolWork.ExtraNonce2 %v", en2, s.PoolWork.ExtraNonce2)
	extraNonce := append(en1[:], en2[:]...)
	poolLog.Tracef("extraNonce %v", extraNonce)

	s.ID++
	sub.ID = s.ID
	s.submitID = s.ID
	s.submitted = true

	poolLog.Tracef("ntime %v", s.PoolWork.Ntime)

	poolLog.Tracef("raw User %v JobId %v xnonce2 %v xnonce2length %v time %v nonce %v", s.User, s.PoolWork.JobID, s.PoolWork.ExtraNonce2, s.PoolWork.ExtraNonce2Length, submittedHeader.Timestamp, submittedHeader.Nonce)

	poolLog.Tracef("encoded User %v JobId %v xnonce2 %v time %v nonce %v", s.User, s.PoolWork.JobID, en2, string(time), nonce)

	sub.Params = []string{s.User, s.PoolWork.JobID, hex.EncodeToString(en2), s.PoolWork.Ntime, nonce}
	// pool->user, work->job_id + 8, xnonce2str, ntimestr, noncestr, nvotestr

	return sub, nil
}

// Various helper functions for formatting are below.

func encodeTime(t time.Time) []byte {
	buf := make([]byte, 8)
	u := uint64(t.Unix())
	binary.BigEndian.PutUint64(buf, u)
	return buf
}

func reverseS(s string) (string, error) {
	a := strings.Split(s, "")
	sRev := ""
	if len(a)%2 != 0 {
		return "", fmt.Errorf("Incorrect input length")
	}
	for i := 0; i < len(a); i += 2 {
		tmp := []string{a[i], a[i+1], sRev}
		sRev = strings.Join(tmp, "")
	}
	return sRev, nil
}

func reverseToInt(s string) (int32, error) {
	sRev, err := reverseS(s)
	if err != nil {
		return 0, err
	}
	i, err := strconv.ParseInt(sRev, 10, 32)
	return int32(i), err
}

func (s *Stratum) diffToTarget(diff float64) string {
	// diff/0 would be bad.
	if s.Diff == 0 {
		s.Diff = 1
	}
	// Also if diff wasn't set properly go with default
	// rather then end if div by 0.
	if diff == 0 {
		diff = 1
	}
	diffNew := int64(diff / s.Diff)
	_, targetHex := s.getTargetHex(diffNew)
	return targetHex
}

// Adapted from https://github.com/sammy007/go-cryptonote-pool.git
func (s *Stratum) getTargetHex(diff int64) (uint32, string) {
	var Diff1 *big.Int
	Diff1 = new(big.Int)
	Diff1.SetString("00000000FFFF0000000000000000000000000000000000000000000000000000", 16)

	padded := make([]byte, 32)

	diff2 := new(big.Int)
	diff2.SetInt64(int64(diff))

	diff3 := new(big.Int)
	diff3 = diff3.Div(Diff1, diff2)

	diffBuff := diff3.Bytes()
	copy(padded[32-len(diffBuff):], diffBuff)
	buff := padded[0:32]
	var target uint32
	targetBuff := bytes.NewReader(buff)
	binary.Read(targetBuff, binary.LittleEndian, &target)
	targetHex := hex.EncodeToString(buff)

	return target, targetHex
}

func reverse(src []byte) []byte {
	dst := make([]byte, len(src))
	for i := len(src); i > 0; i-- {
		dst[len(src)-i] = src[i-1]
	}
	return dst
}

func revHash(hash string) string {
	revHash := ""
	for i := 0; i < 7; i++ {
		j := i * 8
		part := fmt.Sprintf("%c%c%c%c%c%c%c%c", hash[6+j], hash[7+j], hash[4+j], hash[5+j], hash[2+j], hash[3+j], hash[0+j], hash[1+j])
		revHash += part
	}
	return revHash

}
