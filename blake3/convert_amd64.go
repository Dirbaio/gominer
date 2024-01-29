// Copyright (c) 2023 The Decred developers.
//
// Written and optimized by Dave Collins Aug 2023.

package blake3

import "unsafe"

// intoWords writes the provided data in b to the provided array of uint32
// words.
//
// The data in b MUST NOT exceed 64 bytes for a correct result.
func intoWords(words *[16]uint32, b []byte) {
	wordBytes := (*[64]byte)(unsafe.Pointer(words))[:]
	copy(wordBytes, b)
}

// asBytes converts the provided array of uint32 words into bytes.
func asBytes(cv [8]uint32) [32]byte {
	return *(*[32]byte)(unsafe.Pointer(&cv))
}
