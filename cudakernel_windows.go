// Copyright (c) 2016 The Decred developers.
//go:build cuda
// +build cuda

package main

import (
	"syscall"
)

var (
	kernelDll      = syscall.MustLoadDLL("blake3-decred.dll")
	kernelProcAddr = kernelDll.MustFindProc("decred_blake3_hash").Addr()
)
