// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This is a simple server to provide static responses similar to a stratum
// server for debug purposes.

package main

import (
	"fmt"
	"net"
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
	msg1 := `{"id":1,"result":[[["mining.set_difficulty","deadbeefcafebabecc7e1c0000000000"],["mining.notify","deadbeefcafebabecc7e1c0000000000"]],"00000000000000000fe43fbb",12],"error":null}`
	msg2 := `{"id":null,"method":"mining.set_difficulty","params":[8]}`
	msg3 := `{"id":null,"method":"mining.notify","params":["bb3b","6ea8e28a4b172946d743eb3382785120990fe73c247e3dbd000004fc00000000","b25bc74bba24acd4729e61c9f4c53e4f457dc3082d2d28355ae6e6df65e54b4a2040ba54288130410bbcfc548b13711b039bc89f17b3bacb6532bd9001e183f101005c141421c2b80400000097a50000d9f8171ad0357a0f0100000069a3000081460000dbca76570000000000000000","",[],"01000000","1a17f8d9","5776cadb",true]}`
	// WorkData generated from that should be:
	// 010000008ae2a86e4629174b33eb43d7205178823ce70f99bd3d7e24fc04000000000000b25bc74bba24acd4729e61c9f4c53e4f457dc3082d2d28355ae6e6df65e54b4a2040ba54288130410bbcfc548b13711b039bc89f17b3bacb6532bd9001e183f101005c141421c2b80400000097a50000d9f8171ad0357a0f0100000069a3000081460000dbca7657000000000000000000188fec0fe43fbb000000000000000000000000000000000000000000000000

	buf := make([]byte, 1024)
	_, err := c.Read(buf)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
	}

	fmt.Println(string(buf))

	send("subscribe reply", []byte(msg1), c)
	send("difficulty", []byte(msg2), c)
	send("notify", []byte(msg3), c)

	//c.Close()
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
