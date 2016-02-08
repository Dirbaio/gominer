package main

import (
	"encoding/hex"
	"sync"

	"github.com/Dirbaio/gominer/cl"
)

func getCLPlatforms() []cl.CL_platform_id {
	var numPlatforms cl.CL_uint
	status := cl.CLGetPlatformIDs(0, nil, &numPlatforms)
	if status != cl.CL_SUCCESS {
		println("CLGetPlatformIDs status!=cl.CL_SUCCESS")
		return nil
	}
	platforms := make([]cl.CL_platform_id, numPlatforms)
	status = cl.CLGetPlatformIDs(numPlatforms, platforms, nil)
	if status != cl.CL_SUCCESS {
		println("CLGetPlatformIDs status!=cl.CL_SUCCESS")
		return nil
	}
	return platforms
}

// getCLDevices returns the list of devices for the given platform.
func getCLDevices(platform cl.CL_platform_id) []cl.CL_device_id {
	var numDevices cl.CL_uint
	status := cl.CLGetDeviceIDs(platform, cl.CL_DEVICE_TYPE_GPU, 0, nil, &numDevices)
	if status != cl.CL_SUCCESS {
		println("CLGetDeviceIDs status!=cl.CL_SUCCESS")
		return nil
	}
	devices := make([]cl.CL_device_id, numDevices)
	status = cl.CLGetDeviceIDs(platform, cl.CL_DEVICE_TYPE_ALL, numDevices, devices, nil)
	if status != cl.CL_SUCCESS {
		println("CLGetDeviceIDs status!=cl.CL_SUCCESS")
		return nil
	}
	return devices
}

type Miner struct {
	devices []*Device
}

func NewMiner() (*Miner, error) {
	m := &Miner{}

	platformIDs := getCLPlatforms()
	platformID := platformIDs[0]
	deviceIDs := getCLDevices(platformID)

	m.devices = make([]*Device, len(deviceIDs))
	for i, deviceID := range deviceIDs {
		var err error
		m.devices[i], err = NewDevice(i, platformID, deviceID)
		if err != nil {
			return nil, err
		}
	}

	return m, nil
}

var workHex string = "000000008ead1ba5db498d5ae862a1a84198d0fd98fb879f81a28cf40c471700000000000b4d4027bcde0f368d20369fd0c2f88f92e1f36f68188d89b3744146f045e88f73a22c712ef27d99d2aa0bf681259fe70841d80d9c3d03f932c00f0c17e2dd3a01009c9a1d6fcae805000100671900000122481b1ba1643100000000d0210000450a0000396cb656000000000000000000000000000000000000000000000000000000000000000000000000000000008000000100000000000005a0"

func (m *Miner) Run() {
	var wg sync.WaitGroup
	wg.Add(len(m.devices))

	work, _ := hex.DecodeString(workHex)

	for _, d := range m.devices {
		device := d
		device.SetWork(work)
		go func() {
			device.Run()
			device.Release()
			wg.Done()
		}()
	}

	wg.Wait()
}

func (m *Miner) Stop() {
	for _, d := range m.devices {
		d.Stop()
	}

}
