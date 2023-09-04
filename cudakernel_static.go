// Copyright (c) 2016 The Decred developers.

//go:build (linux && cuda) || (darwin && cuda)
// +build linux,cuda darwin,cuda

package main

/*
#include "decred.h"
*/
import "C"
