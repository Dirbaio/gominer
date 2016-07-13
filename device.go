package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"os"
	"unsafe"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg/chainhash"

	"github.com/decred/gominer/blake256"
	"github.com/decred/gominer/cl"
)

const (
	outputBufferSize = cl.CL_size_t(64)
	localWorksize    = 64
	uint32Size       = cl.CL_size_t(unsafe.Sizeof(cl.CL_uint(0)))

	nonce0Word = 3
	nonce1Word = 4
	nonce2Word = 5
)

var zeroSlice = []cl.CL_uint{cl.CL_uint(0)}

func loadProgramSource(filename string) ([][]byte, []cl.CL_size_t, error) {
	var program_buffer [1][]byte
	var program_size [1]cl.CL_size_t

	/* Read each program file and place content into buffer array */
	program_handle, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer program_handle.Close()

	fi, err := program_handle.Stat()
	if err != nil {
		return nil, nil, err
	}
	program_size[0] = cl.CL_size_t(fi.Size())
	program_buffer[0] = make([]byte, program_size[0])
	read_size, err := program_handle.Read(program_buffer[0])
	if err != nil || cl.CL_size_t(read_size) != program_size[0] {
		return nil, nil, err
	}

	return program_buffer[:], program_size[:], nil
}

type Work struct {
	Data   [192]byte
	Target [32]byte
}

type Device struct {
	index        int
	platformID   cl.CL_platform_id
	deviceID     cl.CL_device_id
	deviceName   string
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

func clError(status cl.CL_int, f string) error {
	return fmt.Errorf("%s returned error %s (%d)", f, cl.ERROR_CODES_STRINGS[-status], status)
}

func NewDevice(index int, platformID cl.CL_platform_id, deviceID cl.CL_device_id, workDone chan []byte) (*Device, error) {
	d := &Device{
		index:      index,
		platformID: platformID,
		deviceID:   deviceID,
		deviceName: getDeviceInfo(deviceID, cl.CL_DEVICE_NAME, "CL_DEVICE_NAME"),
		quit:       make(chan struct{}),
		newWork:    make(chan *Work, 5),
		workDone:   workDone,
	}

	var status cl.CL_int

	// Create the CL context
	d.context = cl.CLCreateContext(nil, 1, []cl.CL_device_id{deviceID}, nil, nil, &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateContext")
	}

	// Create the command queue
	d.queue = cl.CLCreateCommandQueue(d.context, deviceID, 0, &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateCommandQueue")
	}

	// Create the output buffer
	d.outputBuffer = cl.CLCreateBuffer(d.context, cl.CL_MEM_READ_WRITE, uint32Size*outputBufferSize, nil, &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateBuffer")
	}

	// Load kernel source
	progSrc, progSize, err := loadProgramSource(cfg.ClKernel)
	if err != nil {
		return nil, fmt.Errorf("Could not load kernel source: %v", err)
	}

	// Create the program
	d.program = cl.CLCreateProgramWithSource(d.context, 1, progSrc[:], progSize[:], &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateProgramWithSource")
	}

	// Build the program for the device
	compilerOptions := ""
	compilerOptions += fmt.Sprintf(" -D WORKSIZE=%d", localWorksize)
	status = cl.CLBuildProgram(d.program, 1, []cl.CL_device_id{deviceID}, []byte(compilerOptions), nil, nil)
	if status != cl.CL_SUCCESS {
		err = clError(status, "CLBuildProgram")

		// Something went wrong! Print what it is.
		var logSize cl.CL_size_t
		status = cl.CLGetProgramBuildInfo(d.program, deviceID, cl.CL_PROGRAM_BUILD_LOG, 0, nil, &logSize)
		if status != cl.CL_SUCCESS {
			minrLog.Errorf("Could not obtain compilation error log: %v", clError(status, "CLGetProgramBuildInfo"))
		}
		var program_log interface{}
		status = cl.CLGetProgramBuildInfo(d.program, deviceID, cl.CL_PROGRAM_BUILD_LOG, logSize, &program_log, nil)
		if status != cl.CL_SUCCESS {
			minrLog.Errorf("Could not obtain compilation error log: %v", clError(status, "CLGetProgramBuildInfo"))
		}
		minrLog.Errorf("%s\n", program_log)

		return nil, err
	}

	// Create the kernel
	d.kernel = cl.CLCreateKernel(d.program, []byte("search"), &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateKernel")
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
	err := d.runDevice()
	if err != nil {
		minrLog.Errorf("Error on device: %v", err)
	}
}

func (d *Device) runDevice() error {
	minrLog.Infof("Started GPU #%d: %s", d.index, d.deviceName)
	outputData := make([]uint32, outputBufferSize)
	globalWorksize := math.Exp2(float64(cfg.Intensity))
	minrLog.Debugf("Intensity %v", cfg.Intensity)
	var status cl.CL_int
	for {
		d.updateCurrentWork()

		select {
		case <-d.quit:
			return nil
		default:
		}

		// Increment nonce1
		d.lastBlock[nonce1Word]++

		// arg 0: pointer to the buffer
		obuf := d.outputBuffer
		status = cl.CLSetKernelArg(d.kernel, 0, cl.CL_size_t(unsafe.Sizeof(obuf)), unsafe.Pointer(&obuf))
		if status != cl.CL_SUCCESS {
			return clError(status, "CLSetKernelArg")
		}

		// args 1..8: midstate
		for i := 0; i < 8; i++ {
			ms := d.midstate[i]
			status = cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+1), uint32Size, unsafe.Pointer(&ms))
			if status != cl.CL_SUCCESS {
				return clError(status, "CLSetKernelArg")
			}
		}

		// args 9..20: lastBlock except nonce
		i2 := 0
		for i := 0; i < 12; i++ {
			if i2 == nonce0Word {
				i2++
			}
			lb := d.lastBlock[i2]
			status = cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+9), uint32Size, unsafe.Pointer(&lb))
			if status != cl.CL_SUCCESS {
				return clError(status, "CLSetKernelArg")
			}
			i2++
		}

		// Clear the found count from the buffer
		status = cl.CLEnqueueWriteBuffer(d.queue, d.outputBuffer, cl.CL_FALSE, 0, uint32Size, unsafe.Pointer(&zeroSlice[0]), 0, nil, nil)
		if status != cl.CL_SUCCESS {
			return clError(status, "CLEnqueueWriteBuffer")
		}

		// Execute the kernel
		var globalWorkSize [1]cl.CL_size_t
		globalWorkSize[0] = cl.CL_size_t(globalWorksize)
		var localWorkSize [1]cl.CL_size_t
		localWorkSize[0] = localWorksize
		status = cl.CLEnqueueNDRangeKernel(d.queue, d.kernel, 1, nil, globalWorkSize[:], localWorkSize[:], 0, nil, nil)
		if status != cl.CL_SUCCESS {
			return clError(status, "CLEnqueueNDRangeKernel")
		}

		// Read the output buffer
		cl.CLEnqueueReadBuffer(d.queue, d.outputBuffer, cl.CL_TRUE, 0, uint32Size*outputBufferSize, unsafe.Pointer(&outputData[0]), 0, nil, nil)
		if status != cl.CL_SUCCESS {
			return clError(status, "CLEnqueueReadBuffer")
		}

		for i := uint32(0); i < outputData[0]; i++ {
			minrLog.Debugf("Found candidate: %d", outputData[i+1])
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

	newHash, err := chainhash.NewHashFromStr(hex.EncodeToString(reverse(hash[:])))
	if err != nil {
		minrLog.Error(err)
	}
	hashNum := blockchain.ShaHashToBig(newHash)
	target := new(big.Int)
	target.SetString(hex.EncodeToString(reverse(d.work.Target[:])), 16)
	if hashNum.Cmp(target) > 0 {
		minrLog.Infof("Hash %s below target %s", hex.EncodeToString(reverse(hash[:])), hex.EncodeToString(reverse(d.work.Target[:])))

	} else {
		minrLog.Infof("Found hash!!  %s", hex.EncodeToString(hash[:]))
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
		return fmt.Sprintf("%.1fGH/s", h/1000000000)
	} else if h > 1000000 {
		return fmt.Sprintf("%.0fMH/s", h/1000000)
	} else if h > 1000 {
		return fmt.Sprintf("%.1fkH/s", h/1000)
	} else if h == 0 {
		return "0H/s"
	}

	return fmt.Sprintf("%.1f GH/s", h)
}

func getDeviceInfo(id cl.CL_device_id,
	name cl.CL_device_info,
	str string) string {

	var errNum cl.CL_int
	var paramValueSize cl.CL_size_t

	errNum = cl.CLGetDeviceInfo(id, name, 0, nil, &paramValueSize)

	if errNum != cl.CL_SUCCESS {
		return fmt.Sprintf("Failed to find OpenCL device info %s.\n", str)
	}

	var info interface{}
	errNum = cl.CLGetDeviceInfo(id, name, paramValueSize, &info, nil)
	if errNum != cl.CL_SUCCESS {
		return fmt.Sprintf("Failed to find OpenCL device info %s.\n", str)
	}

	strinfo := fmt.Sprintf("%v", info)

	return strinfo
}

func (d *Device) PrintStats() {
	alpha := 0.95
	d.workDoneEMA = d.workDoneEMA*alpha + d.workDoneLast*(1-alpha)
	d.workDoneLast = 0
	d.runningTime += 5.0

	minrLog.Infof("GPU #%d: %s, EMA %s avg %s", d.index, d.deviceName,
		formatHashrate(d.workDoneEMA), formatHashrate(d.workDoneTotal/d.runningTime))
}
