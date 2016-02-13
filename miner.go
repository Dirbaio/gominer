package main

import (
	"sync"
	"time"

	"github.com/Dirbaio/gominer/cl"
)

const benchmark = true

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
	devices          []*Device
	workDone         chan []byte
	quit             chan struct{}
	needsWorkRefresh chan struct{}
	wg               sync.WaitGroup
}

func NewMiner() (*Miner, error) {
	m := &Miner{
		workDone:         make(chan []byte, 10),
		quit:             make(chan struct{}),
		needsWorkRefresh: make(chan struct{}),
	}

	platformIDs := getCLPlatforms()
	platformID := platformIDs[0]
	deviceIDs := getCLDevices(platformID)

	m.devices = make([]*Device, len(deviceIDs))
	for i, deviceID := range deviceIDs {
		var err error
		m.devices[i], err = NewDevice(i, platformID, deviceID, m.workDone)
		if err != nil {
			return nil, err
		}
	}

	return m, nil
}

func (m *Miner) workSubmitThread() {
	defer m.wg.Done()

	for {
		select {
		case <-m.quit:
			return
		case data := <-m.workDone:
			accepted, err := GetWorkSubmit(data)
			if err != nil {
				println("Error submitting work:", err.Error())
			} else {
				println("Submitted work, accepted:", accepted)
				m.needsWorkRefresh <- struct{}{}
			}
		}
	}
}

func (m *Miner) workRefreshThread() {
	defer m.wg.Done()

	t := time.NewTicker(time.Second)
	defer t.Stop()

	for {
		work, err := GetWork()
		if err != nil {
			println("Error getwork:", err.Error())
		} else {
			for _, d := range m.devices {
				d.SetWork(work)
			}
		}

		select {
		case <-m.quit:
			return
		case <-t.C:
		case <-m.needsWorkRefresh:
		}
	}
}

func (m *Miner) printStatsThread() {
	defer m.wg.Done()

	t := time.NewTicker(time.Second)
	defer t.Stop()

	for {
		for _, d := range m.devices {
			d.PrintStats()
		}

		select {
		case <-m.quit:
			return
		case <-t.C:
		case <-m.needsWorkRefresh:
		}
	}
}

func (m *Miner) Run() {
	m.wg.Add(len(m.devices))

	for _, d := range m.devices {
		device := d
		go func() {
			device.Run()
			device.Release()
			m.wg.Done()
		}()
	}

	m.wg.Add(1)
	go m.workSubmitThread()
	if benchmark {
		work := &Work{}
		for _, d := range m.devices {
			d.SetWork(work)
		}
	} else {
		m.wg.Add(1)
		go m.workRefreshThread()
	}

	m.wg.Add(1)
	go m.printStatsThread()

	m.wg.Wait()
}

func (m *Miner) Stop() {
	close(m.quit)
	for _, d := range m.devices {
		d.Stop()
	}
}
