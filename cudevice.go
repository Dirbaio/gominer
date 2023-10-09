// Copyright (c) 2016-2023 The Decred developers.

//go:build cuda && !opencl
// +build cuda,!opencl

package main

// The following go:generate directive produces the appropriate intermediate
// library with the Blake3 CUDA kernel for use with gominer as a result of
// executing `go generate -tags cuda .`.
//go:generate go run -tags "cudabuilder" cuda_builder.go

/*
#include "decred.h"

#cgo !windows LDFLAGS: obj/blake3.a
#cgo windows LDFLAGS: -L. -lblake3
*/
import "C"

import (
	"context"
	"fmt"
	"math"
	"math/bits"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/barnex/cuda5/cu"

	"github.com/decred/gominer/nvml"
	"github.com/decred/gominer/util"
	"github.com/decred/gominer/work"
)

const (
	// maxOutputNbs is the max number of individual output results. MUST
	// match what is defined in decred.cu.
	maxOutputResults = 32
)

// Return the GPU library in use.
func gpuLib() string {
	return "CUDA"
}

type Device struct {
	// The following variables must only be used atomically.
	fanPercent  uint32
	temperature uint32

	sync.Mutex
	index int
	cuda  bool

	deviceName               string
	deviceType               string
	fanTempActive            bool
	fanControlActive         bool
	fanControlLastTemp       uint32
	fanControlLastFanPercent uint32
	kind                     string
	tempTarget               uint32

	// Items for CUDA device
	cuDeviceID    cu.Device
	cuThreadCount uint32
	cuGridSize    uint32

	// extraNonce is an additional nonce that is used to separate groups of
	// devices into exclusive ranges to ensure multiple groups do not duplicate
	// work.
	//
	// For solo mining, it is unique per device.
	//
	// For pool mining, it is assigned by the pool on a per-connection basis and
	// therefore is only unique per client.  Note that this means it will be the
	// same for all devices with pool mining.
	extraNonce uint32

	// extraNonce2 is a per device additional nonce where the first byte is the
	// device ID (offset by a per-process random value) and the last 3 bytes are
	// dedicated to the search space.  Note that this means up to 256 devices
	// are supported without the possibility of duplicate work.
	//
	// Since the first byte is unique per device, it does not change during
	// operation which implies this value will rollover to 0x??000000 from
	// 0x??ffffff.
	extraNonce2 uint32

	midstate  [8]uint32
	lastBlock [16]uint32

	work     work.Work
	newWork  chan *work.Work
	workDone chan []byte
	hasWork  bool

	started          uint32
	allDiffOneShares uint64
	validShares      uint64
	invalidShares    uint64
}

func decredBlake3Hash(dimgrid, threads uint32, midstate, lastblock unsafe.Pointer, out cu.DevicePtr) {
	C.decred_blake3_hash(C.uint(dimgrid), C.uint(threads),
		(*C.uint)(midstate),
		(*C.uint)(lastblock),
		(*C.uint)(unsafe.Pointer(out)))
}

func deviceStats(index int) (uint32, uint32) {
	fanPercent := uint32(0)
	temperature := uint32(0)

	dh, err := nvml.DeviceGetHandleByIndex(index)
	if err != nil {
		minrLog.Errorf("NVML DeviceGetHandleByIndex error: %v", err)
		return fanPercent, temperature
	}

	nvmlFanSpeed, err := nvml.DeviceFanSpeed(dh)
	if err != nil {
		minrLog.Infof("NVML DeviceFanSpeed error: %v", err)
	} else {
		fanPercent = uint32(nvmlFanSpeed)
	}

	nvmlTemp, err := nvml.DeviceTemperature(dh)
	if err != nil {
		minrLog.Infof("NVML DeviceTemperature error: %v", err)
	} else {
		temperature = uint32(nvmlTemp)
	}

	return fanPercent, temperature
}

// ithOrFirstInt returns s[index] if len(s) > index or s[0] if not.
func ithOrFirstInt(s []int, index int) int {
	if index < len(s) {
		return s[index]
	}
	return s[0]
}

// unsupported -- just here for compilation
func fanControlSet(index int, fanCur uint32, tempTargetType string,
	fanChangeLevel string) {
	minrLog.Errorf("NVML fanControl() reached but shouldn't have been")
}

func getInfo() ([]cu.Device, error) {
	cu.Init(0)
	ids := cu.DeviceGetCount()
	minrLog.Infof("%v GPUs", ids)
	var CUdevices []cu.Device
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
		return nil, fmt.Errorf("driver does not support CUDA %v.%v API", minMajor, minMinor)
	}

	var numDevices int
	numDevices = cu.DeviceGetCount()
	if numDevices < 1 {
		return nil, fmt.Errorf("no devices found")
	}
	devices := make([]cu.Device, numDevices)
	for i := 0; i < numDevices; i++ {
		dev := cu.DeviceGet(i)
		devices[i] = dev
	}
	return devices, nil
}

// ListDevices prints a list of CUDA capable GPUs present.
func ListDevices() {
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

	devProps := cu.DeviceGetProperties(deviceID)
	minrLog.Infof("CUDA device %.2x props: MaxThreadsPerBlock: %d, MaxThreadsDim: %v, "+
		"MaxGridSize: %v, RegsPerBlock: %d", deviceID, devProps.MaxThreadsPerBlock,
		devProps.MaxThreadsDim, devProps.MaxGridSize, devProps.RegsPerBlock)

	d := &Device{
		index:       index,
		cuDeviceID:  deviceID,
		deviceName:  deviceID.Name(),
		deviceType:  DeviceTypeGPU,
		cuda:        true,
		kind:        DeviceKindNVML,
		newWork:     make(chan *work.Work, 5),
		workDone:    workDone,
		fanPercent:  0,
		temperature: 0,
		tempTarget:  0,
	}

	if !deviceLibraryInitialized {
		err := nvml.Init()
		if err != nil {
			minrLog.Errorf("NVML Init error: %v", err)
		} else {
			deviceLibraryInitialized = true
		}
	}
	fanPercent, temperature := deviceStats(d.index)
	// Newer cards will idle with the fan off so just check if we got
	// a good temperature reading
	if temperature != 0 {
		atomic.StoreUint32(&d.fanPercent, fanPercent)
		atomic.StoreUint32(&d.temperature, temperature)
		d.fanTempActive = true
	}

	// Check if temperature target is specified
	if len(cfg.TempTargetInts) > 0 {
		// Apply the first setting as a global setting
		d.tempTarget = cfg.TempTargetInts[0]

		// Override with the per-device setting if it exists
		for i := range cfg.TempTargetInts {
			if i == order {
				d.tempTarget = uint32(cfg.TempTargetInts[order])
			}
		}
		d.fanControlActive = true
	}

	// validate that we can actually do fan control
	fanControlNotWorking := false
	if d.tempTarget > 0 {
		// validate that fan control is supported
		if !d.fanControlSupported(d.kind) {
			return nil, fmt.Errorf("temperature target of %v for device #%v; "+
				"fan control is not supported on device kind %v", d.tempTarget,
				index, d.kind)
		}
		if !d.fanTempActive {
			minrLog.Errorf("DEV #%d ignoring temperature target of %v; "+
				"could not get initial %v read", index, d.tempTarget, d.kind)
			fanControlNotWorking = true
		}
		if fanControlNotWorking {
			d.tempTarget = 0
			d.fanControlActive = false
		}
	}

	// Use the max nb of threads by default.
	threadCount := uint32(devProps.MaxThreadsPerBlock)
	if len(cfg.CudaThreadCountInts) > 0 {
		threadCount = uint32(ithOrFirstInt(cfg.CudaThreadCountInts, order))
		if threadCount > uint32(devProps.MaxThreadsPerBlock) {
			return nil, fmt.Errorf("specified CUDA thread count %d "+
				"greater than maximum allowed by device #%d (%d)",
				threadCount, deviceID, devProps.MaxThreadsPerBlock)
		}
	}

	// Autocalibrate the desired grid size for the device.
	var gridSize uint32
	autocalibrate := len(cfg.CudaGridSize) == 0
	if autocalibrate {
		var err error
		calibrateTime := ithOrFirstInt(cfg.AutocalibrateInts, order)
		gridSize, err = d.calcGridSizeForMilliseconds(calibrateTime, threadCount)
		if err != nil {
			return nil, err
		}

		minrLog.Infof("Autocalibration successful, grid size for %v"+
			"ms per kernel execution on device %v with %d threads "+
			"determined to be %v",
			calibrateTime, d.index, threadCount, gridSize)
	} else {
		gridSize = uint32(ithOrFirstInt(cfg.CudaGridSizeInts, order))
	}

	d.cuGridSize = gridSize
	d.cuThreadCount = threadCount
	d.started = uint32(time.Now().Unix())
	return d, nil
}

func (d *Device) runDevice(ctx context.Context) error {
	// Initialize the nonces for the device such that each device in the same
	// system is doing different work while also helping prevent collisions
	// across multiple processes and systems working on the same template.
	if err := d.initNonces(); err != nil {
		return err
	}

	// Setup the device settings.
	runtime.LockOSThread()
	cu.DeviceReset()
	cu.SetDevice(d.cuDeviceID)
	cu.SetDeviceFlags(cu.DeviceScheduleBlockingSync)
	defer func() {
		runtime.UnlockOSThread()
		cu.DeviceReset()
	}()

	// kernel is built with nvcc, not an api call so must be done
	// at compile time.

	minrLog.Infof("Started GPU #%d: %s", d.index, d.deviceName)

	const WORDSZ = 4 // Everything is sent as uint32.

	// Setup input buffers.
	midstateSz := int64(len(d.midstate) * WORDSZ)
	midstateH := cu.MallocHost(midstateSz)
	defer cu.MemFreeHost(midstateH)
	midstateHSliceHeader := reflect.SliceHeader{
		Data: uintptr(midstateH),
		Len:  int(midstateSz),
		Cap:  int(midstateSz),
	}
	midstateHSlice := *(*[]uint32)(unsafe.Pointer(&midstateHSliceHeader))

	lastBlockSz := int64(len(d.lastBlock) * WORDSZ)
	lastBlockH := cu.MallocHost(lastBlockSz)
	defer cu.MemFreeHost(lastBlockH)
	lastBlockHSliceHeader := reflect.SliceHeader{
		Data: uintptr(lastBlockH),
		Len:  int(lastBlockSz),
		Cap:  int(lastBlockSz),
	}
	lastBlockHSlice := *(*[]uint32)(unsafe.Pointer(&lastBlockHSliceHeader))

	// Setup output buffer.
	nonceResultsH := cu.MallocHost(maxOutputResults * WORDSZ)
	nonceResultsD := cu.Malloc(maxOutputResults * WORDSZ)
	defer cu.MemFreeHost(nonceResultsH)
	defer nonceResultsD.Free()
	nonceResultsHSliceHeader := reflect.SliceHeader{
		Data: uintptr(nonceResultsH),
		Len:  int(maxOutputResults),
		Cap:  int(maxOutputResults),
	}
	nonceResultsHSlice := *(*[]uint32)(unsafe.Pointer(&nonceResultsHSliceHeader))

	// Mining loop.
	ctxDoneCh := ctx.Done()
	for {
		d.updateCurrentWork(ctx)

		select {
		case <-ctxDoneCh:
			return nil
		default:
		}

		// Increment second extra nonce while respecting the device id.
		util.RolloverExtraNonce(&d.extraNonce2)
		d.lastBlock[work.Nonce2Word] = d.extraNonce2

		// Update the timestamp. Only solo work allows you to roll
		// the timestamp.
		ts := d.work.JobTime
		if d.work.IsGetWork {
			diffSeconds := uint32(time.Now().Unix()) - d.work.TimeReceived
			ts = d.work.JobTime + diffSeconds
		}
		d.lastBlock[work.TimestampWord] = ts

		// Clear the results buffer.
		nonceResultsHSlice[0] = 0
		cu.MemcpyHtoD(nonceResultsD, nonceResultsH, maxOutputResults*WORDSZ)

		// Copy data into the input buffers.
		copy(midstateHSlice, d.midstate[:])
		copy(lastBlockHSlice, d.lastBlock[:])

		// Execute the kernel and follow its execution time.
		currentTime := time.Now()
		decredBlake3Hash(d.cuGridSize, d.cuThreadCount, midstateH, lastBlockH, nonceResultsD)

		// Copy results back from device to host.
		cu.MemcpyDtoH(nonceResultsH, nonceResultsD, maxOutputResults*WORDSZ)

		// Verify the results.
		numResults := nonceResultsHSlice[0]
		for i, result := range nonceResultsHSlice[1 : 1+numResults] {
			minrLog.Debugf("GPU #%d: Found candidate %v nonce %08x, "+
				"extraNonce %08x, extraNonce2 %08x, timestamp %08x",
				d.index, i, result, d.lastBlock[work.Nonce1Word],
				d.lastBlock[work.Nonce2Word], d.lastBlock[work.TimestampWord])

			// Assess the work. If it's below target, it'll be rejected
			// here. The mining algorithm currently sends this function any
			// difficulty 1 shares.
			d.foundCandidate(d.lastBlock[work.TimestampWord], result,
				d.lastBlock[work.Nonce1Word], d.lastBlock[work.Nonce2Word])
		}

		elapsedTime := time.Since(currentTime)
		minrLog.Tracef("GPU #%d: Kernel execution to read time: %v", d.index,
			elapsedTime)
	}
}

// getKernelExecutionTime returns the kernel execution time for a device.
func (d *Device) getKernelExecutionTime(gridSize, threadCount uint32) (time.Duration,
	error) {

	const WORDSZ = 4 // Everything is sent as uint32.

	// Setup input buffers.
	midstateSz := int64(len(d.midstate) * WORDSZ)
	midstateH := cu.MallocHost(midstateSz)
	defer cu.MemFreeHost(midstateH)
	midstateHSliceHeader := reflect.SliceHeader{
		Data: uintptr(midstateH),
		Len:  int(midstateSz),
		Cap:  int(midstateSz),
	}
	midstateHSlice := *(*[]uint32)(unsafe.Pointer(&midstateHSliceHeader))

	lastBlockSz := int64(len(d.lastBlock) * WORDSZ)
	lastBlockH := cu.MallocHost(lastBlockSz)
	defer cu.MemFreeHost(lastBlockH)
	lastBlockHSliceHeader := reflect.SliceHeader{
		Data: uintptr(lastBlockH),
		Len:  int(lastBlockSz),
		Cap:  int(lastBlockSz),
	}
	lastBlockHSlice := *(*[]uint32)(unsafe.Pointer(&lastBlockHSliceHeader))

	// Setup output buffer.
	nonceResultsH := cu.MallocHost(maxOutputResults * WORDSZ)
	nonceResultsD := cu.Malloc(maxOutputResults * WORDSZ)
	defer cu.MemFreeHost(nonceResultsH)
	defer nonceResultsD.Free()
	nonceResultsHSliceHeader := reflect.SliceHeader{
		Data: uintptr(nonceResultsH),
		Len:  int(maxOutputResults),
		Cap:  int(maxOutputResults),
	}
	nonceResultsHSlice := *(*[]uint32)(unsafe.Pointer(&nonceResultsHSliceHeader))

	// Clear the results buffer.
	nonceResultsHSlice[0] = 0
	cu.MemcpyHtoD(nonceResultsD, nonceResultsH, maxOutputResults*WORDSZ)

	// Copy data into the input buffers.
	copy(midstateHSlice, d.midstate[:])
	copy(lastBlockHSlice, d.lastBlock[:])

	// Execute the kernel and follow its execution time.
	currentTime := time.Now()
	decredBlake3Hash(gridSize, threadCount, midstateH, lastBlockH, nonceResultsD)
	cu.MemcpyDtoH(nonceResultsH, nonceResultsD, maxOutputResults*WORDSZ)
	elapsedTime := time.Since(currentTime)
	minrLog.Tracef("DEV #%d: Kernel execution to read time for work "+
		"size calibration: %v", d.index, elapsedTime)

	return elapsedTime, nil
}

// calcWorkSizeForMilliseconds calculates the correct worksize to achieve
// a device execution cycle of the passed duration in milliseconds.
func (d *Device) calcGridSizeForMilliseconds(ms int, threadCount uint32) (uint32, error) {

	// Setup the device settings.
	runtime.LockOSThread()
	cu.DeviceReset()
	cu.SetDevice(d.cuDeviceID)
	cu.SetDeviceFlags(cu.DeviceScheduleBlockingSync)
	defer func() {
		runtime.UnlockOSThread()
		cu.DeviceReset()
	}()

	gridSize := uint32(32)
	timeToAchieve := time.Duration(ms) * time.Millisecond
	for {
		execTime, err := d.getKernelExecutionTime(gridSize, threadCount)
		if err != nil {
			return 0, err
		}

		// If we fail to go above the desired execution time, double
		// the grid size and try again.
		if execTime < timeToAchieve && gridSize < 1<<30 {
			gridSize <<= 1
			continue
		}

		// The lastest call passed the desired execution time, so now
		// calculate what the ideal work size should be.
		adj := float64(gridSize) * (float64(timeToAchieve) / float64(execTime))
		adj /= 256.0
		adjMultiple256 := uint32(math.Ceil(adj))
		gridSize = adjMultiple256 * 256

		// Clamp the gridsize if it will cause the nonce to overflow an
		// uint32 (allowing this would cause duplicated hashing effort).
		if bits.Len32(threadCount-1)+bits.Len32(gridSize-1) > 32 {
			gridSize = 1 << (32 - bits.Len32(threadCount-1))
		}

		// Size it to the nearest multiple of 32 for best CUDA performance.
		gridSize = gridSize - (gridSize % 32)
		if gridSize < 32 {
			gridSize = 32
		}

		break
	}

	return gridSize, nil
}
func newMinerDevs(m *Miner) (*Miner, int, error) {
	deviceListIndex := 0
	deviceListEnabledCount := 0

	CUdeviceIDs, err := getInfo()
	if err != nil {
		return nil, 0, err
	}

	// XXX Can probably combine these bits with the opencl ones once
	// I decide what to do about the types.

	for _, CUDeviceID := range CUdeviceIDs {
		miningAllowed := false

		// Enforce device restrictions if they exist
		if len(cfg.DeviceIDs) > 0 {
			for _, i := range cfg.DeviceIDs {
				if deviceListIndex == i {
					miningAllowed = true
				}
			}
		} else {
			miningAllowed = true
		}

		if miningAllowed {
			newDevice, err := NewCuDevice(deviceListIndex, deviceListEnabledCount, CUDeviceID, m.workDone)
			deviceListEnabledCount++
			m.devices = append(m.devices, newDevice)
			if err != nil {
				return nil, 0, err
			}
		}
		deviceListIndex++
	}

	return m, deviceListEnabledCount, nil
}

func (d *Device) Release() {
	cu.SetDevice(d.cuDeviceID)
	cu.DeviceReset()
}
