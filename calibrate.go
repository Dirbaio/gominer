// Copyright (c) 2016 The Decred developers.

package main

import (
	"math"
	"time"
	"unsafe"

	"github.com/decred/gominer/cl"
	"github.com/decred/gominer/work"
)

// getKernelExecutionTime returns the kernel execution time for a device.
func (d *Device) getKernelExecutionTime(globalWorksize uint32) (time.Duration,
	error) {
	d.work = work.Work{}

	minrLog.Tracef("Started DEV #%d: %s for kernel execution time fetch",
		d.index, d.deviceName)
	outputData := make([]uint32, outputBufferSize)

	var status cl.CL_int

	// arg 0: pointer to the buffer
	obuf := d.outputBuffer
	status = cl.CLSetKernelArg(d.kernel, 0,
		cl.CL_size_t(unsafe.Sizeof(obuf)),
		unsafe.Pointer(&obuf))
	if status != cl.CL_SUCCESS {
		return time.Duration(0), clError(status, "CLSetKernelArg")
	}

	// args 1..8: midstate
	for i := 0; i < 8; i++ {
		ms := d.midstate[i]
		status = cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+1),
			uint32Size, unsafe.Pointer(&ms))
		if status != cl.CL_SUCCESS {
			return time.Duration(0), clError(status, "CLSetKernelArg")
		}
	}

	// args 9..20: lastBlock except nonce
	i2 := 0
	for i := 0; i < 12; i++ {
		if i2 == work.Nonce0Word {
			i2++
		}
		lb := d.lastBlock[i2]
		status = cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+9),
			uint32Size, unsafe.Pointer(&lb))
		if status != cl.CL_SUCCESS {
			return time.Duration(0), clError(status, "CLSetKernelArg")
		}
		i2++
	}

	// Clear the found count from the buffer
	status = cl.CLEnqueueWriteBuffer(d.queue, d.outputBuffer,
		cl.CL_FALSE, 0, uint32Size, unsafe.Pointer(&zeroSlice[0]),
		0, nil, nil)
	if status != cl.CL_SUCCESS {
		return time.Duration(0), clError(status, "CLEnqueueWriteBuffer")
	}

	// Execute the kernel and follow its execution time.
	currentTime := time.Now()
	var globalWorkSize [1]cl.CL_size_t
	globalWorkSize[0] = cl.CL_size_t(globalWorksize)
	var localWorkSize [1]cl.CL_size_t
	localWorkSize[0] = localWorksize
	status = cl.CLEnqueueNDRangeKernel(d.queue, d.kernel, 1, nil,
		globalWorkSize[:], localWorkSize[:], 0, nil, nil)
	if status != cl.CL_SUCCESS {
		return time.Duration(0), clError(status, "CLEnqueueNDRangeKernel")
	}

	// Read the output buffer.
	cl.CLEnqueueReadBuffer(d.queue, d.outputBuffer, cl.CL_TRUE, 0,
		uint32Size*outputBufferSize, unsafe.Pointer(&outputData[0]), 0,
		nil, nil)
	if status != cl.CL_SUCCESS {
		return time.Duration(0), clError(status, "CLEnqueueReadBuffer")
	}

	elapsedTime := time.Since(currentTime)
	minrLog.Tracef("DEV #%d: Kernel execution to read time for work "+
		"size calibration: %v", d.index, elapsedTime)

	return elapsedTime, nil
}

// calcWorkSizeForMilliseconds calculates the correct worksize to achieve
// a device execution cycle of the passed duration in milliseconds.
func (d *Device) calcWorkSizeForMilliseconds(ms int) (uint32, error) {
	workSize := uint32(1 << 10)
	timeToAchieve := time.Duration(ms) * time.Millisecond
	for {
		execTime, err := d.getKernelExecutionTime(workSize)
		if err != nil {
			return 0, err
		}

		// If we fail to go above the desired execution time, double
		// the work size and try again.
		if execTime < timeToAchieve {
			workSize <<= 1
			continue
		}

		// We're passed the desired execution time, so now calculate
		// what the ideal work size should be.
		adj := float64(workSize) * (float64(timeToAchieve) / float64(execTime))
		adj /= 256.0
		adjMultiple256 := uint32(math.Ceil(adj))
		workSize = adjMultiple256 * 256

		break
	}

	return workSize, nil
}
