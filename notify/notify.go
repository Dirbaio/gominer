// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This is a simple server to provide static responses similar to a stratum
// server for debug purposes.

package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)

func main() {
	ln, err := net.Listen("tcp", ":2222")
	if err != nil {
		fmt.Println(err)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println(err)
		}
		go handleConnection(conn)
	}

}

func handleConnection(c net.Conn) {
	msg1 := `{"id":1,"result":[[["mining.set_difficulty","1"],["mining.notify","2bd595e34826a3b6271400920d4decb8"]],"0000000000000000e3014335",12],"error":null}`
	msg2 := `{"id":2,"result":true,"error":null}`
	msg3 := `{"id":null,"method":"mining.set_difficulty","params":[1]}`
	msg4 := `{"id":3,"result":true,"error":null}`
	msg5 := `{"id":null,"method":"mining.notify","params":["76df","7c3b9a506a98f865820e4c46aaa65cec37f18cf1bf7c508700000ac200000000","a455f69725e9c8623baa3c9c5a708aefb947702dc2b620b4c10129977e104c0275571a5ca5b1308b075fe74224504c9e6b1153f3de97235e7a8c7e58ea8f1c55010086a1d41fb3ee05000000fda400004a33121a2db33e1101000000abae0000260800008ec783570000000000000000","",[],"01000000","1a12334a","5783c78e",true]}`
	// WorkData generated from that should be:
	// 010000008ae2a86e4629174b33eb43d7205178823ce70f99bd3d7e24fc04000000000000b25bc74bba24acd4729e61c9f4c53e4f457dc3082d2d28355ae6e6df65e54b4a2040ba54288130410bbcfc548b13711b039bc89f17b3bacb6532bd9001e183f101005c141421c2b80400000097a50000d9f8171ad0357a0f0100000069a3000081460000dbca7657000000000000000000f808120fe43fbb000000000000000000000000000000000000000000000000
	msg6 := `{"id":4,"result":true,"error":null}`

	reader := bufio.NewReader(c)

	for {
		buf, err := reader.ReadBytes('\n')
		if err != nil {
			c.Close()
			return
		}
		fmt.Println("Received " + string(buf))

		if strings.Contains(string(buf), "mining.submit") {
			send("mining.submit reply", []byte(msg6), c)
		} else {
			send("subscribe reply", []byte(msg1), c)
			send("authorize reply", []byte(msg2), c)
			send("difficulty", []byte(msg3), c)
			send("mining.extranonce.subscribe", []byte(msg4), c)
			send("notify", []byte(msg5), c)
		}
	}
}

func send(mType string, m []byte, c net.Conn) {
	fmt.Println("Sending ", mType)
	_, err := c.Write(m)
	if err != nil {
		fmt.Println(err)
	}
	_, err = c.Write([]byte("\n"))
	if err != nil {
		fmt.Println(err)
	}

}
