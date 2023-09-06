// Copyright (c) 2023 The Decred developers.
//
// Decred BLAKE3 midstate-based kernel
//
// Written and optimized by Dave Collins Aug 2023.

// Package blake3 provides a minimal implementation of BLAKE3 that accepts
// midstates and is tailored specifically to Decred.
package blake3

import (
	"math/bits"
)

// BLAKE3 domain separation flags.
const (
	FlagChunkStart = 1
	flagChunkEnd   = 2
	flagRoot       = 8
)

// IV is the BLAKE3 initialization vector.
var IV = [8]uint32{
	0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
	0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
}

// g is the quarter round function that each round applies to the 4x4 internal
// state in the compression function.
func g(a, b, c, d, mx, my uint32) (uint32, uint32, uint32, uint32) {
	a += b + mx
	d = bits.RotateLeft32(d^a, -16)
	c += d
	b = bits.RotateLeft32(b^c, -12)
	a += b + my
	d = bits.RotateLeft32(d^a, -8)
	c += d
	b = bits.RotateLeft32(b^c, -7)
	return a, b, c, d
}

// compress is a stripped down version of the BLAKE3 node compression function
// that will only work properly with a single chunk and truncates the output to
// 256 bits.
func compress(cv [8]uint32, block [16]uint32, blockLen, flags uint32) [8]uint32 {
	// The compression func initializes the 16-word internal state as follows:
	//
	// h0..h7 is the input chaining value (cv).
	//
	// iv0..iv3 are the first 4 words of the constant initialization vector.
	//
	// t0 and t1 are the lower and higher order words of a 64-bit counter, but
	// since only a single chunk is ever hashed in this stripped down version,
	// it's always zero here.
	//
	// b is the number of input bytes in the block (blockLen).
	//
	// d is the domain separation bit flags (flags).
	//
	// |v0  v1  v2  v3 |   |h0  h1  h2  h3 |
	// |v4  v5  v6  v7 |   |h4  h5  h6  h7 |
	// |v8  v9  v10 v11| = |iv0 iv1 iv2 iv3|
	// |v12 v13 v14 v15|   |t0  t1  b   d  |
	//
	// Each round consists of 8 applications of the G function as follows:
	// G0(v0,v4,v8,v12)   G1(v1,v5,v9,v13)   G2(v2,v6,v10,v14)  G3(v3,v7,v11,v15)
	// G4(v0,v5,v10,v15)  G5(v1,v6,v11,v12)  G6(v2,v7,v8,v13)   G7(v3,v4,v9,v14)
	//
	// In other words, the G function is applied to each column of the 4x4 state
	// and then to each of the diagonals.
	//
	// In addition, after each of the first 6 rounds, the message words are
	// permuted according to the following table:
	//
	// 0  1  2  3  4  5  6  7  8  9  10 11 12 13 14 15
	// 2  6  3  10 7  0  4  13 1  11 12 5  9  14 15 8

	// Do the initialization and first round together.
	v0, v4, v8, v12 := g(cv[0], cv[4], IV[0], 0, block[0], block[1])
	v1, v5, v9, v13 := g(cv[1], cv[5], IV[1], 0, block[2], block[3])
	v2, v6, v10, v14 := g(cv[2], cv[6], IV[2], blockLen, block[4], block[5])
	v3, v7, v11, v15 := g(cv[3], cv[7], IV[3], flags, block[6], block[7])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[8], block[9])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[10], block[11])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[12], block[13])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[14], block[15])

	// 2nd round with message word permutation.
	v0, v4, v8, v12 = g(v0, v4, v8, v12, block[2], block[6])
	v1, v5, v9, v13 = g(v1, v5, v9, v13, block[3], block[10])
	v2, v6, v10, v14 = g(v2, v6, v10, v14, block[7], block[0])
	v3, v7, v11, v15 = g(v3, v7, v11, v15, block[4], block[13])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[1], block[11])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[12], block[5])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[9], block[14])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[15], block[8])

	// 3rd round with message word permutation.
	v0, v4, v8, v12 = g(v0, v4, v8, v12, block[3], block[4])
	v1, v5, v9, v13 = g(v1, v5, v9, v13, block[10], block[12])
	v2, v6, v10, v14 = g(v2, v6, v10, v14, block[13], block[2])
	v3, v7, v11, v15 = g(v3, v7, v11, v15, block[7], block[14])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[6], block[5])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[9], block[0])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[11], block[15])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[8], block[1])

	// 4th round with message word permutation.
	v0, v4, v8, v12 = g(v0, v4, v8, v12, block[10], block[7])
	v1, v5, v9, v13 = g(v1, v5, v9, v13, block[12], block[9])
	v2, v6, v10, v14 = g(v2, v6, v10, v14, block[14], block[3])
	v3, v7, v11, v15 = g(v3, v7, v11, v15, block[13], block[15])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[4], block[0])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[11], block[2])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[5], block[8])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[1], block[6])

	// 5th round with message word permutation.
	v0, v4, v8, v12 = g(v0, v4, v8, v12, block[12], block[13])
	v1, v5, v9, v13 = g(v1, v5, v9, v13, block[9], block[11])
	v2, v6, v10, v14 = g(v2, v6, v10, v14, block[15], block[10])
	v3, v7, v11, v15 = g(v3, v7, v11, v15, block[14], block[8])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[7], block[2])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[5], block[3])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[0], block[1])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[6], block[4])

	// 6th round with message word permutation.
	v0, v4, v8, v12 = g(v0, v4, v8, v12, block[9], block[14])
	v1, v5, v9, v13 = g(v1, v5, v9, v13, block[11], block[5])
	v2, v6, v10, v14 = g(v2, v6, v10, v14, block[8], block[12])
	v3, v7, v11, v15 = g(v3, v7, v11, v15, block[15], block[1])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[13], block[3])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[0], block[10])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[2], block[6])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[4], block[7])

	// 7th round with message word permutation.
	v0, v4, v8, v12 = g(v0, v4, v8, v12, block[11], block[15])
	v1, v5, v9, v13 = g(v1, v5, v9, v13, block[5], block[0])
	v2, v6, v10, v14 = g(v2, v6, v10, v14, block[1], block[9])
	v3, v7, v11, v15 = g(v3, v7, v11, v15, block[8], block[6])
	v0, v5, v10, v15 = g(v0, v5, v10, v15, block[14], block[10])
	v1, v6, v11, v12 = g(v1, v6, v11, v12, block[2], block[12])
	v2, v7, v8, v13 = g(v2, v7, v8, v13, block[3], block[4])
	v3, v4, v9, v14 = g(v3, v4, v9, v14, block[7], block[13])

	// Finally the output is defined as:
	//
	// h'0 = v0^v8   h'8  = v8^h0
	// h'1 = v1^v9   h'9  = v9^h1
	// h'2 = v2^v10  h'10 = v10^h2
	// h'3 = v3^v11  h'11 = v11^h3
	// h'4 = v4^v12  h'12 = v12^h4
	// h'5 = v5^v13  h'13 = v13^h5
	// h'6 = v6^v14  h'14 = v14^h6
	// h'7 = v7^v15  h'15 = v15^h7
	//
	// However, the upper results are ignored since only the first 256 bits are
	// needed in this stripped down version.
	return [8]uint32{
		0: v0 ^ v8,
		1: v1 ^ v9,
		2: v2 ^ v10,
		3: v3 ^ v11,
		4: v4 ^ v12,
		5: v5 ^ v13,
		6: v6 ^ v14,
		7: v7 ^ v15,
	}
}

// block runs a single iteration of the BLAKE3 block compression function on the
// provided block in b using the provided domain-specific flags and returns the
// 256-bit truncated state to be chained into the next iteration for the next
// block of data (aka the midstate).
//
// The data in b MUST NOT exceed 64 bytes for a correct result.
func block(midstate [8]uint32, b []byte, flags uint32) [8]uint32 {
	var block [16]uint32
	intoWords(&block, b)
	return compress(midstate, block, uint32(len(b)), flags)
}

// Block runs a single iteration of the BLAKE3 block compression function on the
// provided 64-byte block in b using the provided domain-specific flags and
// returns the 256-bit truncated state to be chained into the next iteration for
// the next block of data (aka the midstate).
//
// The data in b MUST be 64 bytes.  The first iteration must pass the exported
// IV for the midstate and FlagChunkStart for the flags to signal the start of
// the chunk, while the second must pass the midstate returned from the first
// iteration as well as 0 for the flags.
//
// This function is purpose built with the expectation that it will only be
// run on two blocks.  It is not guaranteed to produce correct results in other
// scenarios.
func Block(midstate [8]uint32, b []byte, flags uint32) [8]uint32 {
	return block(midstate, b, flags)
}

// FinalBlock returns the finalized BLAKE3 hash for the given midstate and
// provided portion of the final block to hash.
//
// The data in b MUST NOT exceed 64 bytes for a correct result.
func FinalBlock(midstate [8]uint32, b []byte) [32]byte {
	return asBytes(block(midstate, b, flagChunkEnd|flagRoot))
}
