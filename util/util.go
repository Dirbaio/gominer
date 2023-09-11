// Copyright (c) 2016-2023 The Decred developers.

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
		return "", fmt.Errorf("incorrect input length")
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
	rev := []rune(hash)
	for i := 0; i <= len(rev)/2-2; i += 2 {
		opp := len(rev) - 2 - i
		rev[i], rev[opp] = rev[opp], rev[i]
		rev[i+1], rev[opp+1] = rev[opp+1], rev[i+1]
	}

	return string(rev)
}

// DiffToTarget converts a whole number difficulty into a target.
func DiffToTarget(diff float64, powLimit *big.Int) (*big.Int, error) {
	if diff <= 0 {
		return nil, fmt.Errorf("invalid pool difficulty %v (0 or less than "+
			"zero passed)", diff)
	}

	// Round down in the case of a non-integer diff since we only support
	// ints (unless diff < 1 since we don't allow 0)..
	if diff < 1 {
		diff = 1
	} else {
		diff = math.Floor(diff)
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
	const unit = 1000
	if h < unit {
		return fmt.Sprintf("%.0f h/s", h)
	}
	div, exp := float64(unit), 0
	for n := h / unit; n >= unit && exp < 6; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ch/s", h/div, "kMGTPEZ"[exp])
}
