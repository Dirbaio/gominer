// Copyright (c) 2023 The Decred developers.
//
// Written and optimized by Dave Collins Aug 2023.

//go:build !amd64
// +build !amd64

package blake3

import (
	"encoding/binary"
)

// intoWords writes the provided data in b to the provided array of uint32
// words.
//
// The data in b MUST NOT exceed 64 bytes for a correct result.
func intoWords(words *[16]uint32, b []byte) {
	var block [64]byte
	copy(block[:], b)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(block[4*i:])
	}
}

// asBytes converts the provided array of uint32 words into bytes.
func asBytes(cv [8]uint32) [32]byte {
	var b [32]byte
	for i, v := range cv {
		binary.LittleEndian.PutUint32(b[4*i:], v)
	}
	return b
}
