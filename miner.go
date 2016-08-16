// Copyright (c) 2016 The Decred developers.

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/gominer/stratum"
	"github.com/decred/gominer/work"
)

type Miner struct {
	// The following variables must only be used atomically.
	validShares   uint64
	staleShares   uint64
	invalidShares uint64

	started          uint32
	devices          []*Device
	workDone         chan []byte
	quit             chan struct{}
	needsWorkRefresh chan struct{}
	wg               sync.WaitGroup
	pool             *stratum.Stratum
}

func NewMiner() (*Miner, error) {
	m := &Miner{
		workDone:         make(chan []byte, 10),
		quit:             make(chan struct{}),
		needsWorkRefresh: make(chan struct{}),
	}

	m.devices = make([]*Device, 0)

	// If needed, start pool code.
	if cfg.Pool != "" && !cfg.Benchmark {
		s, err := stratum.StratumConn(cfg.Pool, cfg.PoolUser, cfg.PoolPassword, cfg.Proxy, cfg.ProxyUser, cfg.ProxyPass, version())
		if err != nil {
			return nil, err
		}
		m.pool = s
	}

	if cfg.UseCuda {
		CUdeviceIDs, err := getCUInfo()
		if err != nil {
			return nil, err
		}

		deviceListIndex := 0
		deviceListEnabledCount := 0

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
					return nil, err
				}
			}
			deviceListIndex++
		}

		if deviceListEnabledCount == 0 {
			return nil, fmt.Errorf("No devices started")
		}

	} else {
		platformIDs, err := getCLPlatforms()
		if err != nil {
			return nil, fmt.Errorf("Could not get CL platforms: %v", err)
		}

		deviceListIndex := 0
		deviceListEnabledCount := 0

		for p := range platformIDs {
			platformID := platformIDs[p]
			CLdeviceIDs, err := getCLDevices(platformID)
			if err != nil {
				return nil, fmt.Errorf("Could not get CL devices for platform: %v", err)
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
						return nil, err
					}
					deviceListIndex++
				}
			}

			if deviceListEnabledCount == 0 {
				return nil, fmt.Errorf("No devices started")
			}
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
					atomic.AddUint64(&m.invalidShares, 1)
					minrLog.Errorf("Error submitting work: %v", err)
				} else {
					if accepted {
						atomic.AddUint64(&m.validShares, 1)
						minrLog.Debugf("Submitted work successfully: %v",
							accepted)
					} else {
						atomic.AddUint64(&m.invalidShares, 1)
					}

					m.needsWorkRefresh <- struct{}{}
				}
			} else {
				submitted, err := GetPoolWorkSubmit(data, m.pool)
				if err != nil {
					switch err {
					case stratum.ErrStratumStaleWork:
						atomic.AddUint64(&m.staleShares, 1)
						minrLog.Debugf("Share submitted to pool was stale")

					default:
						atomic.AddUint64(&m.invalidShares, 1)
						minrLog.Errorf("Error submitting work to pool: %v", err)
					}
				} else {
					if submitted {
						minrLog.Debugf("Submitted work to pool successfully: %v",
							submitted)
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
			m.pool.Lock()
			if m.pool.PoolWork.NewWork {
				work, err := GetPoolWork(m.pool)
				m.pool.Unlock()
				if err != nil {
					minrLog.Errorf("Error in getpoolwork: %v", err)
				} else {
					for _, d := range m.devices {
						d.SetWork(work)
					}
				}
			} else {
				m.pool.Unlock()
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
		if cfg.Pool != "" && !cfg.Benchmark {
			valid := atomic.LoadUint64(&m.pool.ValidShares)
			minrLog.Infof("Global stats: Accepted: %v, Rejected: %v, Stale: %v",
				valid,
				atomic.LoadUint64(&m.pool.InvalidShares),
				atomic.LoadUint64(&m.staleShares))

			secondsElapsed := uint32(time.Now().Unix()) - m.started
			if (secondsElapsed / 60) > 0 {
				utility := float64(valid) / (float64(secondsElapsed) / float64(60))
				minrLog.Infof("Global utility (accepted shares/min): %v", utility)
			}
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
		work := &work.Work{}
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
