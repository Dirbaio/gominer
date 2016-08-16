// Copyright (c) 2016 The Decred developers.

package main

/*
#cgo LDFLAGS: -L/opt/cuda/lib64 -L/opt/cuda/lib -lcuda -lcudart -lstdc++ obj/cuda.a
#include <stdint.h>
void decred_hash_nonce(uint32_t grid, uint32_t block, uint32_t threads, uint32_t startNonce, uint32_t *resNonce, uint32_t targetHigh);
void decred_cpu_setBlock_52(const uint32_t *input);
*/
import "C"
import (
	"encoding/binary"
	"fmt"
	"reflect"
	"runtime"
	"time"
	"unsafe"

	"github.com/mumax/3/cuda/cu"

	"github.com/decred/gominer/util"
	"github.com/decred/gominer/work"
)

const (
	// From ccminer
	threadsPerBlock = 640
	blockx          = threadsPerBlock
)

func decredCPUSetBlock52(input *[192]byte) {
	if input == nil {
		panic("input is nil")
	}
	C.decred_cpu_setBlock_52((*C.uint32_t)(unsafe.Pointer(input)))
}

func decredHashNonce(gridx, blockx, threads uint32, startNonce uint32, nonceResults cu.DevicePtr, targetHigh uint32) {
	C.decred_hash_nonce(C.uint32_t(gridx), C.uint32_t(blockx), C.uint32_t(threads),
		C.uint32_t(startNonce), (*C.uint32_t)(unsafe.Pointer(nonceResults)), C.uint32_t(targetHigh))
}

func getCUInfo() ([]cu.Device, error) {
	cu.Init(0)
	ids := cu.DeviceGetCount()
	minrLog.Infof("%v GPUs", ids)
	var CUdevices []cu.Device
	// XXX Do this more like ListCuDevices
	for i := 0; i < ids; i++ {
		dev := cu.DeviceGet(i)
		CUdevices = append(CUdevices, dev)
		minrLog.Infof("%v: %v", i, dev.Name())
	}
	return CUdevices, nil
}

// getCUDevices returns the list of devices for the given platform.
func getCUDevices() ([]cu.Device, error) {
	cu.Init(0)

	version := cu.Version()
	fmt.Println(version)

	maj := version / 1000
	min := version % 100

	minMajor := 5
	minMinor := 5

	if maj < minMajor || (maj == minMajor && min < minMinor) {
		return nil, fmt.Errorf("Driver does not suppoer CUDA %v.%v API", minMajor, minMinor)
	}

	var numDevices int
	numDevices = cu.DeviceGetCount()
	if numDevices < 1 {
		return nil, fmt.Errorf("No devices found")
	}
	devices := make([]cu.Device, numDevices)
	for i := 0; i < numDevices; i++ {
		dev := cu.DeviceGet(i)
		devices[i] = dev
	}
	return devices, nil
}

// ListCuDevices prints a list of CUDA capable GPUs present.
func ListCuDevices() {
	// CUDA devices
	// Because mumux3/3/cuda/cu likes to panic instead of error.
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("No CUDA Capable GPUs present")
		}
	}()
	devices, _ := getCUDevices()
	for i, dev := range devices {
		fmt.Printf("CUDA Capable GPU #%d: %s\n", i, dev.Name())
	}
}

func NewCuDevice(index int, order int, deviceID cu.Device,
	workDone chan []byte) (*Device, error) {

	d := &Device{
		index:      index,
		cuDeviceID: deviceID,
		deviceName: deviceID.Name(),
		cuda:       true,
		quit:       make(chan struct{}),
		newWork:    make(chan *work.Work, 5),
		workDone:   workDone,
	}

	d.cuInSize = 21

	d.started = uint32(time.Now().Unix())

	// Autocalibrate?

	return d, nil

}

func (d *Device) runCuDevice() error {
	// Bump the extraNonce for the device it's running on
	// when you begin mining. This ensures each GPU is doing
	// different work. If the extraNonce has already been
	// set for valid work, restore that.
	d.extraNonce += uint32(d.index) << 24
	d.lastBlock[work.Nonce1Word] = util.Uint32EndiannessSwap(d.extraNonce)

	// Need to have this stuff here for a ctx vs thread issue.
	runtime.LockOSThread()

	// Create the CU context
	d.cuContext = cu.CtxCreate(cu.CTX_BLOCKING_SYNC, d.cuDeviceID)

	// Allocate the input region
	d.cuContext.SetCurrent()

	// kernel is built with nvcc, not an api call so much bet done
	// at compile time.

	minrLog.Infof("Started GPU #%d: %s", d.index, d.deviceName)
	nonceResultsH := cu.MemAllocHost(d.cuInSize * 4)
	nonceResultsD := cu.MemAlloc(d.cuInSize * 4)
	defer cu.MemFreeHost(nonceResultsH)
	defer nonceResultsD.Free()

	nonceResultsHSliceHeader := reflect.SliceHeader{
		Data: uintptr(nonceResultsH),
		Len:  int(d.cuInSize),
		Cap:  int(d.cuInSize),
	}
	nonceResultsHSlice := *(*[]uint32)(unsafe.Pointer(&nonceResultsHSliceHeader))

	endianData := new([192]byte)

	for {
		d.updateCurrentWork()

		select {
		case <-d.quit:
			return nil
		default:
		}

		// Increment extraNonce.
		util.RolloverExtraNonce(&d.extraNonce)
		d.lastBlock[work.Nonce1Word] = util.Uint32EndiannessSwap(d.extraNonce)

		copy(endianData[:], d.work.Data[:128])
		for i, j := 128, 0; i < 180; {
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, d.lastBlock[j])
			copy(endianData[i:], b)
			i += 4
			j++
		}
		decredCPUSetBlock52(endianData)

		// Update the timestamp. Only solo work allows you to roll
		// the timestamp.
		ts := d.work.JobTime
		if d.work.IsGetWork {
			diffSeconds := uint32(time.Now().Unix()) - d.work.TimeReceived
			ts = d.work.JobTime + diffSeconds
		}
		d.lastBlock[work.TimestampWord] = util.Uint32EndiannessSwap(ts)

		nonceResultsHSlice[0] = 0

		cu.MemcpyHtoD(nonceResultsD, nonceResultsH, d.cuInSize*4)

		// Execute the kernel and follow its execution time.
		currentTime := time.Now()

		// TODO Which nonceword is this?  In ccminer it is &pdata[35]
		startNonce := d.lastBlock[work.Nonce1Word]
		//fmt.Printf("%p %v\n", &startNonce, startNonce)

		throughput := uint32(0x20000000) // TODO
		//throughput = minUint32(throughput, ^uint32(0)-nonce)
		//gridx := int((throughput + threadsPerBlock - 1) / threadsPerBlock)
		//gridx := (int(throughput) + 639) / 640
		gridx := ((throughput - 1) / 640)

		gridx = 52428 // don't ask me why this works.

		targetHigh := ^uint32(0) // TODO

		decredHashNonce(gridx, blockx, throughput, startNonce, nonceResultsD, targetHigh)

		cu.MemcpyDtoH(nonceResultsH, nonceResultsD, d.cuInSize*4)

		numResults := nonceResultsHSlice[0]
		for i, result := range nonceResultsHSlice[1 : 1+numResults] {
			// lol seelog
			i := i
			result := result
			minrLog.Debugf("GPU #%d: Found candidate %v nonce %08x, "+
				"extraNonce %08x, workID %08x, timestamp %08x",
				d.index, i, result, d.lastBlock[work.Nonce1Word],
				util.Uint32EndiannessSwap(d.currentWorkID),
				d.lastBlock[work.TimestampWord])

			// Assess the work. If it's below target, it'll be rejected
			// here. The mining algorithm currently sends this function any
			// difficulty 1 shares.
			d.foundCandidate(d.lastBlock[work.TimestampWord], result,
				d.lastBlock[work.Nonce1Word])
		}

		elapsedTime := time.Since(currentTime)
		minrLog.Tracef("GPU #%d: Kernel execution to read time: %v", d.index,
			elapsedTime)
	}

	return nil
}

func minUint32(a, b uint32) uint32 {
	if a > b {
		return a
	} else {
		return b
	}
}
