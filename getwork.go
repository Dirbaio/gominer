package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

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
	user       = "test"
	password   = "test"
	url        = "http://localhost:19109/"
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
	jsonStr := []byte(`{"jsonrpc": "2.0", "method": "getwork", "params": [], "id": 1}`)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if resp.Status != "200 OK" {
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, body)
	}

	var res getWorkResponseJson
	err = json.Unmarshal(body, &res)
	if err != nil {
		return nil, err
	}

	if res.Error != nil {
		return nil, fmt.Errorf("JSONRPC Error %d: %s", res.Error.Code, res.Error.Message)
	}

	data, err := hex.DecodeString(res.Result.Data)
	if err != nil {
		return nil, err
	}
	if len(data) != 192 {
		return nil, fmt.Errorf("Wrong data length: got %d, expected 192", len(data))
	}
	target, err := hex.DecodeString(res.Result.Target)
	if err != nil {
		return nil, err
	}
	if len(target) != 32 {
		return nil, fmt.Errorf("Wrong target length: got %d, expected 32", len(target))
	}

	var w Work
	copy(w.Data[:], data)
	copy(w.Target[:], target)
	return &w, nil
}

// GetWork makes a getwork RPC call and returns the result (data and target)
func GetWorkSubmit(data []byte) (bool, error) {
	hexData := hex.EncodeToString(data)
	jsonStr := []byte(`{"jsonrpc": "2.0", "method": "getwork", "params": ["` + hexData + `"], "id": 1}`)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if resp.Status != "200 OK" {
		return false, fmt.Errorf("error calling getwork (%s): %s", resp.Status, body)
	}

	var res getWorkSubmitResponseJson
	err = json.Unmarshal(body, &res)
	if err != nil {
		return false, err
	}

	if res.Error != nil {
		return false, fmt.Errorf("JSONRPC Error %d: %s", res.Error.Code, res.Error.Message)
	}

	return res.Result, nil
}
