package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"unsafe"

	"github.com/Dirbaio/gominer/blake256"
	"github.com/Dirbaio/gominer/cl"
)

const (
	outputBufferSize = cl.CL_size_t(64)
	globalWorksize   = 65536 * 1024
	localWorksize    = 64
	uint32Size       = cl.CL_size_t(unsafe.Sizeof(cl.CL_uint(0)))

	nonce0Word = 3
	nonce1Word = 4
	nonce2Word = 5
)

var zeroSlice = []cl.CL_uint{cl.CL_uint(0)}

func loadProgramSource(filename string) ([][]byte, []cl.CL_size_t) {
	var program_buffer [1][]byte
	var program_size [1]cl.CL_size_t

	/* Read each program file and place content into buffer array */
	program_handle, err1 := os.Open(filename)
	if err1 != nil {
		fmt.Printf("Couldn't find the program file %s\n", filename)
		return nil, nil
	}
	defer program_handle.Close()

	fi, err2 := program_handle.Stat()
	if err2 != nil {
		fmt.Printf("Couldn't find the program stat\n")
		return nil, nil
	}
	program_size[0] = cl.CL_size_t(fi.Size())
	program_buffer[0] = make([]byte, program_size[0])
	read_size, err3 := program_handle.Read(program_buffer[0])
	if err3 != nil || cl.CL_size_t(read_size) != program_size[0] {
		fmt.Printf("read file error or file size wrong\n")
		return nil, nil
	}

	return program_buffer[:], program_size[:]
}

type Work struct {
	Data   [192]byte
	Target [32]byte
}

type Device struct {
	index        int
	platformID   cl.CL_platform_id
	deviceID     cl.CL_device_id
	context      cl.CL_context
	queue        cl.CL_command_queue
	outputBuffer cl.CL_mem
	program      cl.CL_program
	kernel       cl.CL_kernel

	midstate  [8]uint32
	lastBlock [16]uint32

	work     Work
	newWork  chan *Work
	workDone chan []byte
	hasWork  bool

	workDoneEMA   float64
	workDoneLast  float64
	workDoneTotal float64
	runningTime   float64

	quit chan struct{}
}

// Compares a and b as big endian
func hashSmaller(a, b []byte) bool {
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

func NewDevice(index int, platformID cl.CL_platform_id, deviceID cl.CL_device_id, workDone chan []byte) (*Device, error) {
	d := &Device{
		index:      index,
		platformID: platformID,
		deviceID:   deviceID,
		quit:       make(chan struct{}),
		newWork:    make(chan *Work, 5),
		workDone:   workDone,
	}

	var status cl.CL_int

	// Create the CL context
	d.context = cl.CLCreateContext(nil, 1, []cl.CL_device_id{deviceID}, nil, nil, &status)
	if status != cl.CL_SUCCESS {
		println("CLCreateContext status!=cl.CL_SUCCESS")
		return nil, nil
	}

	// Create the command queue
	d.queue = cl.CLCreateCommandQueue(d.context, deviceID, 0, &status)
	if status != cl.CL_SUCCESS {
		println("CLCreateCommandQueue status!=cl.CL_SUCCESS")
		return nil, nil
	}

	// Create the output buffer
	d.outputBuffer = cl.CLCreateBuffer(d.context, cl.CL_MEM_READ_WRITE, uint32Size*outputBufferSize, nil, &status)
	if status != cl.CL_SUCCESS {
		println("CLCreateBuffer status!=cl.CL_SUCCESS")
		return nil, nil
	}

	// Create the program
	progSrc, progSize := loadProgramSource("blake256.cl")
	d.program = cl.CLCreateProgramWithSource(d.context, 1, progSrc[:], progSize[:], &status)
	if status != cl.CL_SUCCESS {
		println("CLCreateProgramWithSource status!=cl.CL_SUCCESS")
		return nil, nil
	}

	// Build the program for the device
	compilerOptions := ""
	compilerOptions += fmt.Sprintf(" -D WORKSIZE=%d", localWorksize)
	status = cl.CLBuildProgram(d.program, 1, []cl.CL_device_id{deviceID}, []byte(compilerOptions), nil, nil)
	if status != cl.CL_SUCCESS {
		println("CLBuildProgram status!=cl.CL_SUCCESS")
		// Something went wrong! Print what it is.
		var logSize cl.CL_size_t
		status = cl.CLGetProgramBuildInfo(d.program, deviceID, cl.CL_PROGRAM_BUILD_LOG, 0, nil, &logSize)
		var program_log interface{}
		status = cl.CLGetProgramBuildInfo(d.program, deviceID, cl.CL_PROGRAM_BUILD_LOG, logSize, &program_log, nil)
		fmt.Printf("%s\n", program_log)
		return nil, nil
	}

	// Create the kernel
	d.kernel = cl.CLCreateKernel(d.program, []byte("search"), &status)
	if status != cl.CL_SUCCESS {
		println("CLCreateKernel status!=cl.CL_SUCCESS:", status)
		return nil, nil
	}

	return d, nil
}

func (d *Device) Release() {
	cl.CLReleaseKernel(d.kernel)
	cl.CLReleaseProgram(d.program)
	cl.CLReleaseCommandQueue(d.queue)
	cl.CLReleaseMemObject(d.outputBuffer)
	cl.CLReleaseContext(d.context)
}

func (d *Device) updateCurrentWork() {
	var w *Work
	if d.hasWork {
		// If we already have work, we just need to check if there's new one
		// without blocking if there's not.
		select {
		case w = <-d.newWork:
		default:
			return
		}
	} else {
		// If we don't have work, we block until we do. We need to watch for
		// quit events too.
		select {
		case w = <-d.newWork:
		case <-d.quit:
			return
		}
	}

	d.hasWork = true

	d.work = *w

	// Set nonce2
	binary.BigEndian.PutUint32(d.work.Data[128+4*nonce2Word:], uint32(d.index))

	// Reset the hash state
	copy(d.midstate[:], blake256.IV256[:])

	// Hash the two first blocks
	blake256.Block(d.midstate[:], d.work.Data[0:64], 512)
	blake256.Block(d.midstate[:], d.work.Data[64:128], 1024)

	// Convert the next block to uint32 array.
	for i := 0; i < 16; i++ {
		d.lastBlock[i] = binary.BigEndian.Uint32(d.work.Data[128+i*4:])
	}
}

func (d *Device) Run() {
	println("Started GPU #", d.index)
	outputData := make([]uint32, outputBufferSize)
	var status cl.CL_int
	for {
		d.updateCurrentWork()

		select {
		case <-d.quit:
			return
		default:
		}

		// Increment nonce1
		d.lastBlock[nonce1Word]++

		// arg 0: pointer to the buffer
		obuf := d.outputBuffer
		status = cl.CLSetKernelArg(d.kernel, 0, cl.CL_size_t(unsafe.Sizeof(obuf)), unsafe.Pointer(&obuf))
		if status != cl.CL_SUCCESS {
			println("CLSetKernelArg status!=cl.CL_SUCCESS:", status)
			return
		}

		// args 1..8: midstate
		for i := 0; i < 8; i++ {
			ms := d.midstate[i]
			status |= cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+1), uint32Size, unsafe.Pointer(&ms))
		}

		// args 9..20: lastBlock except nonce
		i2 := 0
		for i := 0; i < 12; i++ {
			if i2 == nonce0Word {
				i2++
			}
			lb := d.lastBlock[i2]
			status |= cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+9), uint32Size, unsafe.Pointer(&lb))
			i2++
		}

		if status != cl.CL_SUCCESS {
			println("CLSetKernelArg status!=cl.CL_SUCCESS")
			return
		}

		// Clear the found count from the buffer
		status = cl.CLEnqueueWriteBuffer(d.queue, d.outputBuffer, cl.CL_FALSE, 0, uint32Size, unsafe.Pointer(&zeroSlice[0]), 0, nil, nil)
		if status != cl.CL_SUCCESS {
			println("CLEnqueueWriteBuffer status!=cl.CL_SUCCESS")
			return
		}

		// Execute the kernel
		var globalWorkSize [1]cl.CL_size_t
		globalWorkSize[0] = globalWorksize
		var localWorkSize [1]cl.CL_size_t
		localWorkSize[0] = localWorksize
		status = cl.CLEnqueueNDRangeKernel(d.queue, d.kernel, 1, nil, globalWorkSize[:], localWorkSize[:], 0, nil, nil)
		if status != cl.CL_SUCCESS {
			println("CLEnqueueNDRangeKernel status!=cl.CL_SUCCESS")
			return
		}

		// Read the output buffer
		cl.CLEnqueueReadBuffer(d.queue, d.outputBuffer, cl.CL_TRUE, 0, uint32Size*outputBufferSize, unsafe.Pointer(&outputData[0]), 0, nil, nil)
		if status != cl.CL_SUCCESS {
			println("CLEnqueueReadBuffer status!=cl.CL_SUCCESS")
			return
		}

		for i := uint32(0); i < outputData[0]; i++ {
			println("candidate", outputData[i+1])
			d.foundCandidate(d.lastBlock[nonce1Word], outputData[i+1])
		}

		d.workDoneLast += globalWorksize
		d.workDoneTotal += globalWorksize
	}
}

func (d *Device) foundCandidate(nonce1 uint32, nonce0 uint32) {
	// Construct the final block header
	data := make([]byte, 192)
	copy(data, d.work.Data[:])
	binary.BigEndian.PutUint32(data[128+4*nonce1Word:], nonce1)
	binary.BigEndian.PutUint32(data[128+4*nonce0Word:], nonce0)

	// Perform the final hash block to get the hash
	var state [8]uint32
	copy(state[:], d.midstate[:])
	blake256.Block(state[:], data[128:192], 1440)

	var hash [32]byte
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(hash[i*4:], state[i])
	}

	if hashSmaller(hash[:], d.work.Target[:]) {
		println("FOUND HASH", hex.EncodeToString(hash[:]))
		d.workDone <- data
	}
}

func (d *Device) Stop() {
	close(d.quit)
}

func (d *Device) SetWork(w *Work) {
	d.newWork <- w
}

func formatHashrate(h float64) string {
	if h > 1000000000 {
		return fmt.Sprintf("%.3f GH/s", h/1000000000)
	} else if h > 1000000 {
		return fmt.Sprintf("%.3f MH/s", h/1000000)
	} else if h > 1000 {
		return fmt.Sprintf("%.3f kH/s", h/1000)
	} else {
		return fmt.Sprintf("%.3f GH/s", h)
	}
}

func (d *Device) PrintStats() {
	alpha := 0.95
	d.workDoneEMA = d.workDoneEMA*alpha + d.workDoneLast*(1-alpha)
	d.workDoneLast = 0
	d.runningTime += 1.0
	println("EMA " + formatHashrate(d.workDoneEMA) + ", avg " + formatHashrate(d.workDoneTotal/d.runningTime))
}
