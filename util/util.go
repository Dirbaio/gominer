// Copyright (c) 2016 The Decred developers.

package util

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Reverse reverses a byte array.
func Reverse(src []byte) []byte {
	dst := make([]byte, len(src))
	for i := len(src); i > 0; i-- {
		dst[len(src)-i] = src[i-1]
	}
	return dst
}

// reverseS reverses a hex string.
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

// ReverseToInt reverse a string and converts to int32.
func ReverseToInt(s string) (int32, error) {
	sRev, err := reverseS(s)
	if err != nil {
		return 0, err
	}
	i, err := strconv.ParseInt(sRev, 10, 32)
	return int32(i), err
}

// RevHash reverses a hash in string format.
func RevHash(hash string) string {
	revHash := ""
	for i := 0; i < 7; i++ {
		j := i * 8
		part := fmt.Sprintf("%c%c%c%c%c%c%c%c",
			hash[6+j], hash[7+j], hash[4+j], hash[5+j],
			hash[2+j], hash[3+j], hash[0+j], hash[1+j])
		revHash += part
	}
	return revHash
}

// DiffToTarget converts a whole number difficulty into a target.
func DiffToTarget(diff float64, powLimit *big.Int) (*big.Int, error) {
	if diff <= 0 {
		return nil, fmt.Errorf("invalid pool difficulty %v (0 or less than "+
			"zero passed)", diff)
	}

	if math.Floor(diff) < diff {
		return nil, fmt.Errorf("invalid pool difficulty %v (not a whole "+
			"number)", diff)
	}

	divisor := new(big.Int).SetInt64(int64(diff))
	max := powLimit
	target := new(big.Int)
	target.Div(max, divisor)

	return target, nil
}

// RolloverExtraNonce rolls over the extraNonce if it goes over 0x00FFFFFF many
// hashes, since the first byte is reserved for the ID.
func RolloverExtraNonce(v *uint32) {
	if *v&0x00FFFFFF == 0x00FFFFFF {
		*v = *v & 0xFF000000
	} else {
		*v++
	}
}

// Uint32EndiannessSwap swaps the endianness of a uint32.
func Uint32EndiannessSwap(v uint32) uint32 {
	return (v&0x000000FF)<<24 | (v&0x0000FF00)<<8 |
		(v&0x00FF0000)>>8 | (v&0xFF000000)>>24
}

// FormatHashRate sets the units properly when displaying a hashrate.
func FormatHashRate(h float64) string {
	if h > 1000000000 {
		return fmt.Sprintf("%.3fGH/s", h/1000000000)
	} else if h > 1000000 {
		return fmt.Sprintf("%.0fMH/s", h/1000000)
	} else if h > 1000 {
		return fmt.Sprintf("%.1fkH/s", h/1000)
	} else if h == 0 {
		return "0H/s"
	}

	return fmt.Sprintf("%.1f GH/s", h)
}
