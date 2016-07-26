package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/btcsuite/go-socks/socks"
)

// newHTTPClient returns a new HTTP client that is configured according to the
// proxy and TLS settings in the associated connection configuration.
func newHTTPClient(cfg *config) (*http.Client, error) {
	// Configure proxy if needed.
	var dial func(network, addr string) (net.Conn, error)
	if cfg.Proxy != "" {
		proxy := &socks.Proxy{
			Addr:     cfg.Proxy,
			Username: cfg.ProxyUser,
			Password: cfg.ProxyPass,
		}
		dial = func(network, addr string) (net.Conn, error) {
			c, err := proxy.Dial(network, addr)
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	}

	// Configure TLS if needed.
	var tlsConfig *tls.Config
	if !cfg.NoTLS && cfg.RPCCert != "" {
		pem, err := ioutil.ReadFile(cfg.RPCCert)
		if err != nil {
			return nil, err
		}

		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(pem)
		tlsConfig = &tls.Config{
			RootCAs:            pool,
			InsecureSkipVerify: cfg.TLSSkipVerify,
		}
	}

	// Create and return the new HTTP client potentially configured with a
	// proxy and TLS.
	client := http.Client{
		Transport: &http.Transport{
			Dial:            dial,
			TLSClientConfig: tlsConfig,
		},
	}
	return &client, nil
}

type getWorkResponseJson struct {
	Result struct {
		Data   string
		Target string
	}
	Error *struct {
		Code    int
		Message string
	}
}

type getWorkSubmitResponseJson struct {
	Result bool
	Error  *struct {
		Code    int
		Message string
	}
}

var (
	httpClient *http.Client
)

const (
	MaxIdleConnections int = 20
	RequestTimeout     int = 5
)

// init HTTPClient
func init() {
	httpClient = createHTTPClient()
}

// createHTTPClient for connection re-use
func createHTTPClient() *http.Client {
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: MaxIdleConnections,
		},
		Timeout: time.Duration(RequestTimeout) * time.Second,
	}

	return client
}

// GetWork makes a getwork RPC call and returns the result (data and target)
func GetWork() (*Work, error) {
	// Generate a request to the configured RPC server.
	protocol := "http"
	if !cfg.NoTLS {
		protocol = "https"
	}
	url := protocol + "://" + cfg.RPCServer
	jsonStr := []byte(`{"jsonrpc": "2.0", "method": "getwork", "params": [], "id": 1}`)
	bodyBuff := bytes.NewBuffer(jsonStr)
	httpRequest, err := http.NewRequest("POST", url, bodyBuff)
	if err != nil {
		return nil, err
	}
	httpRequest.Close = true
	httpRequest.Header.Set("Content-Type", "application/json")

	// Configure basic access authorization.
	httpRequest.SetBasicAuth(cfg.RPCUser, cfg.RPCPassword)

	// Create the new HTTP client that is configured according to the user-
	// specified options and submit the request.
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		err = fmt.Errorf("error reading json reply: %v", err)
		return nil, err
	}

	if httpResponse.Status != "200 OK" {
		return nil, fmt.Errorf("HTTP %s: %s", httpResponse.Status, body)
	}

	var res getWorkResponseJson
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	if res.Error != nil {
		return nil, fmt.Errorf("JSONRPC Error %d: %s", res.Error.Code,
			res.Error.Message)
	}

	data, err := hex.DecodeString(res.Result.Data)
	if err != nil {
		return nil, err
	}
	if len(data) != 192 {
		return nil, fmt.Errorf("Wrong data length: got %d, expected 192",
			len(data))
	}
	target, err := hex.DecodeString(res.Result.Target)
	if err != nil {
		return nil, err
	}
	if len(target) != 32 {
		return nil, fmt.Errorf("Wrong target length: got %d, expected 32",
			len(target))
	}

	bigTarget := new(big.Int)
	bigTarget.SetBytes(reverse(target))

	var workData [192]byte
	copy(workData[:], data)
	givenTs := binary.LittleEndian.Uint32(
		workData[128+4*timestampWord : 132+4*timestampWord])
	w := NewWork(workData, bigTarget, givenTs, uint32(time.Now().Unix()), true)

	w.Target = bigTarget

	return w, nil
}

// GetPoolWork gets work from a stratum enabled pool
func GetPoolWork(pool *Stratum) (*Work, error) {
	// Get Next work for stratum and mark it as used
	if pool.PoolWork.NewWork {
		poolLog.Info("Received new work from pool.")
		// Mark used
		pool.PoolWork.NewWork = false

		if pool.PoolWork.JobID == "" {
			return nil, fmt.Errorf("No work available (no job id)")
		}

		err := pool.PrepWork()
		if err != nil {
			return nil, err
		}

		intJob, _ := strconv.ParseInt(pool.PoolWork.JobID, 16, 0)
		poolLog.Debugf("new job %v height %v", intJob, pool.PoolWork.Height)

		return pool.PoolWork.Work, nil
	}

	// Return the work we already had, do not recalculate
	if pool.PoolWork.Work != nil {
		return pool.PoolWork.Work, nil
	}

	return nil, fmt.Errorf("No work available.")
}

// GetWork makes a getwork RPC call and returns the result (data and target)
func GetWorkSubmit(data []byte) (bool, error) {
	// Generate a request to the configured RPC server.
	protocol := "http"
	if !cfg.NoTLS {
		protocol = "https"
	}
	url := protocol + "://" + cfg.RPCServer
	hexData := hex.EncodeToString(data)
	jsonStr := []byte(`{"jsonrpc": "2.0", "method": "getwork", "params": ["` +
		hexData + `"], "id": 1}`)
	bodyBuff := bytes.NewBuffer(jsonStr)
	httpRequest, err := http.NewRequest("POST", url, bodyBuff)
	if err != nil {
		return false, err
	}
	httpRequest.Close = true
	httpRequest.Header.Set("Content-Type", "application/json")

	// Configure basic access authorization.
	httpRequest.SetBasicAuth(cfg.RPCUser, cfg.RPCPassword)

	// Create the new HTTP client that is configured according to the user-
	// specified options and submit the request.
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return false, err
	}
	httpResponse, err := httpClient.Do(httpRequest)
	if err != nil {
		return false, err
	}

	body, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		err = fmt.Errorf("error reading json reply: %v", err)
		return false, err
	}

	if httpResponse.Status != "200 OK" {
		return false, fmt.Errorf("error calling getwork (%s): %s",
			httpResponse.Status, body)
	}

	var res getWorkSubmitResponseJson
	err = json.Unmarshal(body, &res)
	if err != nil {
		return false, err
	}

	if res.Error != nil {
		return false, fmt.Errorf("JSONRPC Error %d: %s", res.Error.Code,
			res.Error.Message)
	}

	return res.Result, nil
}

// GetPoolWorkSubmit sends the result to the stratum enabled pool
func GetPoolWorkSubmit(data []byte, pool *Stratum) (bool, error) {
	sub, err := pool.PrepSubmit(data)
	if err != nil {
		return false, err
	}

	// JSON encode.
	m, err := json.Marshal(sub)
	if err != nil {
		return false, err
	}

	// Send.
	poolLog.Tracef("> %s", m)
	_, err = pool.Conn.Write(m)
	if err != nil {
		return false, err
	}
	_, err = pool.Conn.Write([]byte("\n"))
	if err != nil {
		return false, err
	}

	pool.submitted = true

	return true, nil
}
