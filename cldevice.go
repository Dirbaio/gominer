// Copyright (c) 2016 The Decred developers.

// +build opencl,!cuda,!opencladl

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/decred/gominer/cl"
	"github.com/decred/gominer/util"
	"github.com/decred/gominer/work"

	"golang.org/x/sys/unix"
)

// Return the GPU library in use.
func gpuLib() string {
	return "OpenCL"
}

const (
	outputBufferSize = cl.CL_size_t(64)
	localWorksize    = 64
	uint32Size       = cl.CL_size_t(unsafe.Sizeof(cl.CL_uint(0)))
)

var zeroSlice = []cl.CL_uint{cl.CL_uint(0)}

func appendBitfield(info, value cl.CL_bitfield, name string, str *string) {
	if (info & value) != 0 {
		*str += name
	}
}

func amdgpuFanPercentToValue(percent uint32) uint32 {
	value := AMDGPUFanFailSafe

	calculatedValue := float64(AMDGPUFanMax) * float64(percent) / float64(100)

	if calculatedValue > 0 {
		value = uint32(calculatedValue)
	} else {
		minrLog.Errorf("amdgpuFanPercentToValue() failed; using failsafe "+
			"value of %v", AMDGPUFanFailSafe)
	}

	return value
}

// validate that we can write to the AMDGPU sysfs fan path
func amdgpuFanPermissionsValid(index int) error {
	path := amdgpuGetSysfsPath(index, "fan")

	err := unix.Access(path, unix.W_OK)
	if err != nil {
		return fmt.Errorf("path %v is not writable", path)
	}

	return nil
}

func amdgpuGetSysfsPath(index int, field string) string {
	cardPath := fmt.Sprintf("%s%d", "/sys/class/drm/card", index)
	driverPath := "/sys/module/amdgpu"

	if field == "card" {
		return cardPath
	}
	if field == "driver" {
		return driverPath
	}

	// find hwmon/hwmon<number>
	hwmonBasePath := fmt.Sprintf("%s%d%s", "/sys/class/drm/card", index, "/device/hwmon/")
	hwmonName := ""

	// open hwmon base path and scan for the numbered entry
	files, err := ioutil.ReadDir(hwmonBasePath)
	if err != nil {
		minrLog.Errorf("unable to read AMDGPU sysfs dir %v: %v", hwmonBasePath,
			err)
		return "unknown"
	}

	for _, f := range files {
		// we should only find one entry but the API may not be stable
		if strings.Contains(f.Name(), "hwmon") {
			hwmonName = f.Name()
		}
	}

	if hwmonName == "" {
		minrLog.Errorf("unable to find full hwmon path")
		return "unknown"
	}

	hwmonFullPath := fmt.Sprintf("%s/%s/", hwmonBasePath, hwmonName)

	switch field {
	case "fan":
		return hwmonFullPath + "pwm1"
	case "temp":
		return hwmonFullPath + "temp1_input"
	}

	return "unknown"
}

func fanControlSet(index int, fanCur uint32, tempTargetType string,
	fanChangeLevel string) {
	fanAdjustmentPercent := FanControlAdjustmentSmall
	fanNewPercent := uint32(0)
	fanNewValue := uint32(0)
	if fanChangeLevel == ChangeLevelLarge {
		fanAdjustmentPercent = FanControlAdjustmentLarge
	}
	minrLog.Tracef("DEV #%d fanControlSet fanCur %v tempTargetType %v "+
		"fanChangeLevel %v", index, fanCur, tempTargetType, fanChangeLevel)

	switch tempTargetType {
	// Decrease the temperature by increasing the fan speed
	case TargetLower:
		fanNewPercent = fanCur + fanAdjustmentPercent
		fanNewValue = amdgpuFanPercentToValue(fanNewPercent)
		break
	// Increase the temperature by decreasing the fan speed
	case TargetHigher:
		fanNewPercent = fanCur - fanAdjustmentPercent
		fanNewValue = amdgpuFanPercentToValue(fanNewPercent)
		break
	}

	fanPath := amdgpuGetSysfsPath(index, "fan")

	minrLog.Tracef("DEV #%d need to %v temperature; adjusting fan from "+
		"fanCur %v%% to fanNewPercent %v%% by writing fanNewValue %v to %v",
		index, strings.ToLower(tempTargetType), fanCur, fanNewPercent,
		fanNewValue, fanPath)
	err := deviceStatsWriteSysfsEntry(fanPath, fanNewValue)
	if err != nil {
		minrLog.Errorf("DEV #%d unable to adjust fan: %v", index, err.Error())
	} else {
		minrLog.Infof("DEV #%d successfully adjusted fan from %v%% to %v%% to "+
			"%v temp", index, fanCur, fanNewPercent,
			strings.ToLower(tempTargetType))
	}
}

func loadProgramSource(filename string) ([][]byte, []cl.CL_size_t, error) {
	var programBuffer [1][]byte
	var programSize [1]cl.CL_size_t

	// Read each program file and place content into buffer array.
	programHandle, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	defer programHandle.Close()

	buf := bytes.NewBuffer(nil)
	_, err = io.Copy(buf, programHandle)
	if err != nil {
		return nil, nil, err
	}
	str := string(buf.Bytes())
	programFinal := []byte(str)

	programSize[0] = cl.CL_size_t(len(programFinal))
	programBuffer[0] = make([]byte, programSize[0])
	for i := range programFinal {
		programBuffer[0][i] = programFinal[i]
	}

	return programBuffer[:], programSize[:], nil
}

func clError(status cl.CL_int, f string) error {
	if -status < 0 || int(-status) > len(cl.ERROR_CODES_STRINGS) {
		return fmt.Errorf("returned unknown error")
	}

	return fmt.Errorf("%s returned error %s (%d)", f,
		cl.ERROR_CODES_STRINGS[-status], status)
}

type Device struct {
	// The following variables must only be used atomically.
	fanPercent  uint32
	temperature uint32

	sync.Mutex
	index int
	cuda  bool

	// Items for OpenCL device
	platformID               cl.CL_platform_id
	deviceID                 cl.CL_device_id
	deviceName               string
	deviceType               string
	context                  cl.CL_context
	queue                    cl.CL_command_queue
	outputBuffer             cl.CL_mem
	program                  cl.CL_program
	kernel                   cl.CL_kernel
	fanControlActive         bool
	fanControlLastTemp       uint32
	fanControlLastFanPercent uint32
	fanTempActive            bool
	kind                     string
	tempTarget               uint32

	//cuInput        cu.DevicePtr
	cuInSize       int64
	cuOutputBuffer []float64

	workSize uint32

	// extraNonce is the device extraNonce, where the first
	// byte is the device ID (supporting up to 255 devices)
	// while the last 3 bytes is the extraNonce value. If
	// the extraNonce goes through all 0x??FFFFFF values,
	// it will reset to 0x??000000.
	extraNonce    uint32
	currentWorkID uint32

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

	quit chan struct{}
}

// If the device order and OpenCL index are ever not the same then we can
// implement topology finding code:
// https://github.com/Oblomov/clinfo/blob/master/src/clinfo.c#L1061-L1126
func determineDeviceKind(index int, deviceType string) string {
	deviceKind := DeviceKindUnknown

	if deviceType == DeviceTypeCPU {
		return deviceKind
	}

	switch runtime.GOOS {
	case "linux":
		// check if the AMDGPU driver is loaded
		if _, err := os.Stat(amdgpuGetSysfsPath(index, "driver")); err == nil {
			// make sure a sysfs entry exists for the index of this device
			if _, err := os.Stat(amdgpuGetSysfsPath(index, "card")); err == nil {
				deviceKind = DeviceKindAMDGPU
			}
		}
		break
	}

	return deviceKind
}

func deviceStats(index int) (uint32, uint32) {
	fanPercent := uint32(0)
	temperature := uint32(0)

	fanPercent = deviceStatsReadSysfsEntry(amdgpuGetSysfsPath(index, "fan"))
	fanPercentFloat := float64(fanPercent) / float64(AMDGPUFanMax) * float64(100)
	fanPercent = uint32(fanPercentFloat)
	temperature = deviceStatsReadSysfsEntry(amdgpuGetSysfsPath(index, "temp")) / AMDTempDivisor

	return fanPercent, temperature
}

func deviceStatsReadSysfsEntry(path string) uint32 {
	res := uint32(0)
	dataRaw := ""

	f, err := os.Open(path)
	if err != nil {
		if err != nil {
			minrLog.Errorf("unable to open %v", path)
			return res
		}
	}
	defer f.Close()

	r := bufio.NewScanner(f)
	for r.Scan() {
		dataRaw = string(r.Bytes())
	}
	if err := r.Err(); err != nil {
		return res
	}

	dataInt, err := strconv.Atoi(dataRaw)
	if err != nil {
		minrLog.Errorf("unable to convert to int %v", err)
		return res
	}

	res = uint32(dataInt)

	return res
}

func deviceStatsWriteSysfsEntry(path string, value uint32) error {
	stringValue := strconv.Itoa(int(value)) + "\n"
	err := ioutil.WriteFile(path, []byte(stringValue), 0644)
	if err != nil {
		return fmt.Errorf("unable to write %v to %v: %v", value, path, err)
	}

	return nil
}

func getCLInfo() (cl.CL_platform_id, []cl.CL_device_id, error) {
	var platformID cl.CL_platform_id
	platformIDs, err := getCLPlatforms()
	if err != nil {
		return platformID, nil, fmt.Errorf("Could not get CL platforms: %v", err)
	}
	platformID = platformIDs[0]
	CLdeviceIDs, err := getCLDevices(platformID)
	if err != nil {
		return platformID, nil, fmt.Errorf("Could not get CL devices for platform: %v", err)
	}
	return platformID, CLdeviceIDs, nil
}

func getCLPlatforms() ([]cl.CL_platform_id, error) {
	var numPlatforms cl.CL_uint
	status := cl.CLGetPlatformIDs(0, nil, &numPlatforms)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLGetPlatformIDs")
	}
	platforms := make([]cl.CL_platform_id, numPlatforms)
	status = cl.CLGetPlatformIDs(numPlatforms, platforms, nil)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLGetPlatformIDs")
	}
	return platforms, nil
}

// getCLDevices returns the list of devices for the given platform.
func getCLDevices(platform cl.CL_platform_id) ([]cl.CL_device_id, error) {
	var numDevices cl.CL_uint
	status := cl.CLGetDeviceIDs(platform, cl.CL_DEVICE_TYPE_ALL, 0, nil,
		&numDevices)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLGetDeviceIDs")
	}
	devices := make([]cl.CL_device_id, numDevices)
	status = cl.CLGetDeviceIDs(platform, cl.CL_DEVICE_TYPE_ALL, numDevices,
		devices, nil)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLGetDeviceIDs")
	}
	return devices, nil
}

// ListDevices prints a list of devices present.
func ListDevices() {
	platformIDs, err := getCLPlatforms()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not get CL platforms: %v\n", err)
		os.Exit(1)
	}

	deviceListIndex := 0
	for i := range platformIDs {
		platformID := platformIDs[i]
		deviceIDs, err := getCLDevices(platformID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not get CL devices for platform: %v\n", err)
			os.Exit(1)
		}
		for _, deviceID := range deviceIDs {
			fmt.Printf("DEV #%d: %s\n", deviceListIndex, getDeviceInfo(deviceID, cl.CL_DEVICE_NAME, "CL_DEVICE_NAME"))
			deviceListIndex++
		}

	}
}

func NewDevice(index int, order int, platformID cl.CL_platform_id, deviceID cl.CL_device_id,
	workDone chan []byte) (*Device, error) {
	d := &Device{
		index:       index,
		platformID:  platformID,
		deviceID:    deviceID,
		deviceName:  getDeviceInfo(deviceID, cl.CL_DEVICE_NAME, "CL_DEVICE_NAME"),
		deviceType:  getDeviceInfo(deviceID, cl.CL_DEVICE_TYPE, "CL_DEVICE_TYPE"),
		quit:        make(chan struct{}),
		newWork:     make(chan *work.Work, 5),
		workDone:    workDone,
		fanPercent:  0,
		temperature: 0,
		tempTarget:  0,
	}

	var status cl.CL_int

	// Create the CL context.
	d.context = cl.CLCreateContext(nil, 1, []cl.CL_device_id{deviceID},
		nil, nil, &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateContext")
	}

	// Create the command queue.
	d.queue = cl.CLCreateCommandQueue(d.context, deviceID, 0, &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateCommandQueue")
	}

	// Create the output buffer.
	d.outputBuffer = cl.CLCreateBuffer(d.context, cl.CL_MEM_READ_WRITE,
		uint32Size*outputBufferSize, nil, &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateBuffer")
	}

	// Load kernel source.
	progSrc, progSize, err := loadProgramSource(cfg.ClKernel)
	if err != nil {
		return nil, fmt.Errorf("Could not load kernel source: %v", err)
	}

	// Create the program.
	d.program = cl.CLCreateProgramWithSource(d.context, 1, progSrc[:],
		progSize[:], &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateProgramWithSource")
	}

	// Build the program for the device.
	compilerOptions := ""
	compilerOptions += fmt.Sprintf(" -D WORKSIZE=%d", localWorksize)
	status = cl.CLBuildProgram(d.program, 1, []cl.CL_device_id{deviceID},
		[]byte(compilerOptions), nil, nil)
	if status != cl.CL_SUCCESS {
		err = clError(status, "CLBuildProgram")

		// Something went wrong! Print what it is.
		var logSize cl.CL_size_t
		status = cl.CLGetProgramBuildInfo(d.program, deviceID,
			cl.CL_PROGRAM_BUILD_LOG, 0, nil, &logSize)
		if status != cl.CL_SUCCESS {
			minrLog.Errorf("Could not obtain compilation error log: %v",
				clError(status, "CLGetProgramBuildInfo"))
		}
		var programLog interface{}
		status = cl.CLGetProgramBuildInfo(d.program, deviceID,
			cl.CL_PROGRAM_BUILD_LOG, logSize, &programLog, nil)
		if status != cl.CL_SUCCESS {
			minrLog.Errorf("Could not obtain compilation error log: %v",
				clError(status, "CLGetProgramBuildInfo"))
		}
		minrLog.Errorf("%s\n", programLog)

		return nil, err
	}

	// Create the kernel.
	d.kernel = cl.CLCreateKernel(d.program, []byte("search"), &status)
	if status != cl.CL_SUCCESS {
		return nil, clError(status, "CLCreateKernel")
	}

	d.started = uint32(time.Now().Unix())

	// Autocalibrate the desired work size for the kernel, or use one of the
	// values passed explicitly by the use.
	// The intensity or worksize must be set by the user.
	userSetWorkSize := false
	if len(cfg.IntensityInts) > 0 || len(cfg.WorkSizeInts) > 0 {
		userSetWorkSize = true
	}

	var globalWorkSize uint32
	if !userSetWorkSize {
		// Apply the first setting as a global setting
		calibrateTime := cfg.AutocalibrateInts[0]

		// Override with the per-device setting if it exists
		for i := range cfg.AutocalibrateInts {
			if i == order {
				calibrateTime = cfg.AutocalibrateInts[i]
			}
		}

		idealWorkSize, err := d.calcWorkSizeForMilliseconds(calibrateTime)
		if err != nil {
			return nil, err
		}

		minrLog.Debugf("Autocalibration successful, work size for %v"+
			"ms per kernel execution on device %v determined to be %v",
			calibrateTime, d.index, idealWorkSize)

		globalWorkSize = idealWorkSize
	} else {
		if len(cfg.IntensityInts) > 0 {
			// Apply the first setting as a global setting
			globalWorkSize = 1 << uint32(cfg.IntensityInts[0])

			// Override with the per-device setting if it exists
			for i := range cfg.IntensityInts {
				if i == order {
					globalWorkSize = 1 << uint32(cfg.IntensityInts[order])
				}
			}
		}
		if len(cfg.WorkSizeInts) > 0 {
			// Apply the first setting as a global setting
			globalWorkSize = uint32(cfg.WorkSizeInts[0])

			// Override with the per-device setting if it exists
			for i := range cfg.WorkSizeInts {
				if i == order {
					globalWorkSize = uint32(cfg.WorkSizeInts[order])
				}
			}

		}
	}
	intensity := math.Log2(float64(globalWorkSize))
	minrLog.Infof("DEV #%d: Work size set to %v ('intensity' %v)",
		d.index, globalWorkSize, intensity)
	d.workSize = globalWorkSize

	// Determine the device/driver kind
	d.kind = determineDeviceKind(d.index, d.deviceType)

	switch d.kind {
	case DeviceKindAMDGPU:
		fanPercent, temperature := deviceStats(d.index)
		// Newer cards will idle with the fan off so just check if we got
		// a good temperature reading
		if temperature != 0 {
			atomic.StoreUint32(&d.fanPercent, fanPercent)
			atomic.StoreUint32(&d.temperature, temperature)
			d.fanTempActive = true
		}
		break
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
		if !fanControlNotWorking {
			err := amdgpuFanPermissionsValid(index)
			if err != nil {
				minrLog.Errorf("DEV #%d ignoring temperature target of %v; "+
					"%v", index, d.tempTarget, err)
				fanControlNotWorking = true
			}
		}
		if fanControlNotWorking {
			d.tempTarget = 0
			d.fanControlActive = false
		}
	}

	return d, nil
}

func (d *Device) runDevice() error {
	minrLog.Infof("Started DEV #%d: %s", d.index, d.deviceName)
	outputData := make([]uint32, outputBufferSize)

	// Bump the extraNonce for the device it's running on
	// when you begin mining. This ensures each device is doing
	// different work. If the extraNonce has already been
	// set for valid work, restore that.
	d.extraNonce += uint32(d.index) << 24
	d.lastBlock[work.Nonce1Word] = util.Uint32EndiannessSwap(d.extraNonce)

	var status cl.CL_int
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

		// Update the timestamp. Only solo work allows you to roll
		// the timestamp.
		ts := d.work.JobTime
		if d.work.IsGetWork {
			diffSeconds := uint32(time.Now().Unix()) - d.work.TimeReceived
			ts = d.work.JobTime + diffSeconds
		}
		d.lastBlock[work.TimestampWord] = util.Uint32EndiannessSwap(ts)

		// arg 0: pointer to the buffer
		obuf := d.outputBuffer
		status = cl.CLSetKernelArg(d.kernel, 0,
			cl.CL_size_t(unsafe.Sizeof(obuf)),
			unsafe.Pointer(&obuf))
		if status != cl.CL_SUCCESS {
			return clError(status, "CLSetKernelArg")
		}

		// args 1..8: midstate
		for i := 0; i < 8; i++ {
			ms := d.midstate[i]
			status = cl.CLSetKernelArg(d.kernel, cl.CL_uint(i+1),
				uint32Size, unsafe.Pointer(&ms))
			if status != cl.CL_SUCCESS {
				return clError(status, "CLSetKernelArg")
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
				return clError(status, "CLSetKernelArg")
			}
			i2++
		}

		// Clear the found count from the buffer
		status = cl.CLEnqueueWriteBuffer(d.queue, d.outputBuffer,
			cl.CL_FALSE, 0, uint32Size, unsafe.Pointer(&zeroSlice[0]),
			0, nil, nil)
		if status != cl.CL_SUCCESS {
			return clError(status, "CLEnqueueWriteBuffer")
		}

		// Execute the kernel and follow its execution time.
		currentTime := time.Now()
		var globalWorkSize [1]cl.CL_size_t
		globalWorkSize[0] = cl.CL_size_t(d.workSize)
		var localWorkSize [1]cl.CL_size_t
		localWorkSize[0] = localWorksize
		status = cl.CLEnqueueNDRangeKernel(d.queue, d.kernel, 1, nil,
			globalWorkSize[:], localWorkSize[:], 0, nil, nil)
		if status != cl.CL_SUCCESS {
			return clError(status, "CLEnqueueNDRangeKernel")
		}

		// Read the output buffer.
		cl.CLEnqueueReadBuffer(d.queue, d.outputBuffer, cl.CL_TRUE, 0,
			uint32Size*outputBufferSize, unsafe.Pointer(&outputData[0]), 0,
			nil, nil)
		if status != cl.CL_SUCCESS {
			return clError(status, "CLEnqueueReadBuffer")
		}

		for i := uint32(0); i < outputData[0]; i++ {
			minrLog.Debugf("DEV #%d: Found candidate %v nonce %08x, "+
				"extraNonce %08x, workID %08x, timestamp %08x",
				d.index, i+1, outputData[i+1], d.lastBlock[work.Nonce1Word],
				util.Uint32EndiannessSwap(d.currentWorkID),
				d.lastBlock[work.TimestampWord])

			// Assess the work. If it's below target, it'll be rejected
			// here. The mining algorithm currently sends this function any
			// difficulty 1 shares.
			d.foundCandidate(d.lastBlock[work.TimestampWord], outputData[i+1],
				d.lastBlock[work.Nonce1Word])
		}

		elapsedTime := time.Since(currentTime)
		minrLog.Tracef("DEV #%d: Kernel execution to read time: %v", d.index,
			elapsedTime)
	}
}

func newMinerDevs(m *Miner) (*Miner, int, error) {
	deviceListIndex := 0
	deviceListEnabledCount := 0

	platformIDs, err := getCLPlatforms()
	if err != nil {
		return nil, 0, fmt.Errorf("Could not get CL platforms: %v", err)
	}

	for p := range platformIDs {
		platformID := platformIDs[p]
		CLdeviceIDs, err := getCLDevices(platformID)
		if err != nil {
			return nil, 0, fmt.Errorf("Could not get CL devices for platform: %v", err)
		}

		for _, CLdeviceID := range CLdeviceIDs {
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
				newDevice, err := NewDevice(deviceListIndex, deviceListEnabledCount, platformID, CLdeviceID, m.workDone)
				deviceListEnabledCount++
				m.devices = append(m.devices, newDevice)
				if err != nil {
					return nil, 0, err
				}
			}
			deviceListIndex++
		}
	}
	return m, deviceListEnabledCount, nil

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

	switch name {
	case cl.CL_DEVICE_TYPE:
		var deviceTypeStr string

		appendBitfield(cl.CL_bitfield(info.(cl.CL_device_type)),
			cl.CL_bitfield(cl.CL_DEVICE_TYPE_CPU),
			DeviceTypeCPU,
			&deviceTypeStr)

		appendBitfield(cl.CL_bitfield(info.(cl.CL_device_type)),
			cl.CL_bitfield(cl.CL_DEVICE_TYPE_GPU),
			DeviceTypeGPU,
			&deviceTypeStr)

		info = deviceTypeStr
	}

	strinfo := fmt.Sprintf("%v", info)

	return strinfo
}

func (d *Device) Release() {
	cl.CLReleaseKernel(d.kernel)
	cl.CLReleaseProgram(d.program)
	cl.CLReleaseCommandQueue(d.queue)
	cl.CLReleaseMemObject(d.outputBuffer)
	cl.CLReleaseContext(d.context)
	// XXX need to check if/how the AMDGPU driver/device takes back
	// automatic fan control like we do for ADL
}
