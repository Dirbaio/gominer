// Copyright (c) 2016 The Decred developers.

package main

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/gominer/cl"
)

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
	status := cl.CLGetDeviceIDs(platform, cl.CL_DEVICE_TYPE_GPU, 0, nil,
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

type Miner struct {
	devices          []*Device
	workDone         chan []byte
	quit             chan struct{}
	needsWorkRefresh chan struct{}
	wg               sync.WaitGroup
	pool             *Stratum

	started       uint32
	validShares   uint64
	staleShares   uint64
	invalidShares uint64
}

func NewMiner() (*Miner, error) {
	m := &Miner{
		workDone:         make(chan []byte, 10),
		quit:             make(chan struct{}),
		needsWorkRefresh: make(chan struct{}),
	}

	// If needed, start pool code.
	if cfg.Pool != "" && !cfg.Benchmark {
		s, err := StratumConn(cfg.Pool, cfg.PoolUser, cfg.PoolPassword)
		if err != nil {
			return nil, err
		}
		m.pool = s
	}

	platformIDs, err := getCLPlatforms()
	if err != nil {
		return nil, fmt.Errorf("Could not get CL platforms: %v", err)
	}
	platformID := platformIDs[0]
	deviceIDs, err := getCLDevices(platformID)
	if err != nil {
		return nil, fmt.Errorf("Could not get CL devices for platform: %v", err)
	}

	// Check the number of intensities/work sizes versus the number of devices.
	userSetWorkSize := false
	if reflect.DeepEqual(cfg.Intensity, defaultIntensity) &&
		reflect.DeepEqual(cfg.WorkSize, defaultWorkSize) {
		userSetWorkSize = false
	}
	if userSetWorkSize {
		if reflect.DeepEqual(cfg.WorkSize, defaultWorkSize) {
			if len(cfg.Intensity) != len(deviceIDs) {
				return nil, fmt.Errorf("Intensities supplied, but number supplied "+
					"did not match the number of GPUs (got %v, want %v)",
					len(cfg.Intensity), len(deviceIDs))
			}
		} else {
			if len(cfg.WorkSize) != len(deviceIDs) {
				return nil, fmt.Errorf("WorkSize supplied, but number supplied "+
					"did not match the number of GPUs (got %v, want %v)",
					len(cfg.WorkSize), len(deviceIDs))
			}
		}
	}

	m.devices = make([]*Device, len(deviceIDs))
	for i, deviceID := range deviceIDs {
		var err error
		m.devices[i], err = NewDevice(i, platformID, deviceID, m.workDone)
		if err != nil {
			return nil, err
		}
	}

	m.started = uint32(time.Now().Unix())

	return m, nil
}

func (m *Miner) workSubmitThread() {
	defer m.wg.Done()

	for {
		select {
		case <-m.quit:
			return
		case data := <-m.workDone:
			// Only use that is we are not using a pool.
			if m.pool == nil {
				accepted, err := GetWorkSubmit(data)
				if err != nil {
					inval := atomic.LoadUint64(&m.invalidShares)
					inval++
					atomic.StoreUint64(&m.invalidShares, inval)

					minrLog.Errorf("Error submitting work: %v", err)
				} else {
					if accepted {
						val := atomic.LoadUint64(&m.validShares)
						val++
						atomic.StoreUint64(&m.validShares, val)

						minrLog.Debugf("Submitted work successfully: %v",
							accepted)
					} else {
						inval := atomic.LoadUint64(&m.invalidShares)
						inval++
						atomic.StoreUint64(&m.invalidShares, inval)
					}

					m.needsWorkRefresh <- struct{}{}
				}
			} else {
				accepted, err := GetPoolWorkSubmit(data, m.pool)
				if err != nil {
					if err == ErrStatumStaleWork {
						stale := atomic.LoadUint64(&m.staleShares)
						stale++
						atomic.StoreUint64(&m.staleShares, stale)
					} else {
						inval := atomic.LoadUint64(&m.invalidShares)
						inval++
						atomic.StoreUint64(&m.invalidShares, inval)

						minrLog.Errorf("Error submitting work to pool: %v", err)
					}
				} else {
					if accepted {
						val := atomic.LoadUint64(&m.validShares)
						val++
						atomic.StoreUint64(&m.validShares, val)

						minrLog.Debugf("Submitted work to pool successfully: %v",
							accepted)
					} else {
						inval := atomic.LoadUint64(&m.invalidShares)
						inval++
						atomic.StoreUint64(&m.invalidShares, inval)

						m.invalidShares++
					}
					m.needsWorkRefresh <- struct{}{}
				}
			}
		}
	}
}

func (m *Miner) workRefreshThread() {
	defer m.wg.Done()

	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

	for {
		// Only use that is we are not using a pool.
		if m.pool == nil {
			work, err := GetWork()
			if err != nil {
				minrLog.Errorf("Error in getwork: %v", err)
			} else {
				for _, d := range m.devices {
					d.SetWork(work)
				}
			}
		} else {
			if m.pool.PoolWork.NewWork {
				work, err := GetPoolWork(m.pool)
				if err != nil {
					minrLog.Errorf("Error in getpoolwork: %v", err)
				} else {
					for _, d := range m.devices {
						d.SetWork(work)
					}
				}
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

	t := time.NewTicker(time.Second * 5)
	defer t.Stop()

	for {
		valid := atomic.LoadUint64(&m.validShares)
		minrLog.Infof("Global stats: Accepted: %v, Rejected: %v, Stale: %v",
			valid,
			atomic.LoadUint64(&m.invalidShares),
			atomic.LoadUint64(&m.staleShares))

		secondsElapsed := uint32(time.Now().Unix()) - m.started
		if (secondsElapsed / 60) > 0 {
			utility := float64(valid) / (float64(secondsElapsed) / float64(60))
			minrLog.Infof("Global utility (accepted shares/min): %v", utility)
		}
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

	if cfg.Benchmark {
		minrLog.Warn("Running in BENCHMARK mode! No real mining taking place!")
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
		m.wg.Done()
	}
}
