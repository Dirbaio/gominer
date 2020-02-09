package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/decred/gominer/util"
)

type MinerStatus struct {
	ValidShares     uint64  `json:"validShares"`
	StaleShares     uint64  `json:"staleShares"`
	InvalidShares   uint64  `json:"invalidShares"`
	TotalShares     uint64  `json:"totalShares"`
	SharesPerMinute float64 `json:"sharesPerMinute"`
	Started         uint32  `json:"started"`
	Uptime          uint32  `json:"uptime"`

	Devices []*DeviceStatus `json:"devices"`
	Pool    *PoolStatus     `json:"pool,omitempty"`
}

type DeviceStatus struct {
	Index      int    `json:"index"`
	DeviceName string `json:"deviceName"`
	DeviceType string `json:"deviceType"`

	HashRate          float64 `json:"hashRate"`
	HashRateFormatted string  `json:"hashRateFormatted"`

	FanPercent  uint32 `json:"fanPercent"`
	Temperature uint32 `json:"temperature"`

	Started uint32 `json:"started"`
}

type PoolStatus struct {
	Started uint32 `json:"started"`
	Uptime  uint32 `json:"uptime"`
}

var (
	m *Miner
)

func RunMonitor(tm *Miner) {
	m = tm

	if len(cfg.APIListeners) != 0 {
		http.HandleFunc("/", getMinerStatus)

		for _, addr := range cfg.APIListeners {
			err := http.ListenAndServe(addr, nil)

			if err != nil {
				mainLog.Warnf("Unable to create monitor: %v", err)
				return
			}
		}
	}
}

func getMinerStatus(w http.ResponseWriter, req *http.Request) {
	ms := &MinerStatus{
		Started: m.started,
		Uptime:  uint32(time.Now().Unix()) - m.started,
	}

	if !cfg.Benchmark {
		valid, invalid, stale, total, sharesPerMinute := m.Status()

		ms.ValidShares = valid
		ms.InvalidShares = invalid
		ms.StaleShares = stale
		ms.TotalShares = total
		ms.SharesPerMinute = sharesPerMinute

		if cfg.Pool != "" {
			ms.Pool = &PoolStatus{
				Started: m.started,
				Uptime:  uint32(time.Now().Unix()) - m.started,
			}
		}
	}

	for _, d := range m.devices {
		d.UpdateFanTemp()

		averageHashRate,
			fanPercent,
			temperature := d.Status()

		ms.Devices = append(ms.Devices, &DeviceStatus{
			Index:             d.index,
			DeviceName:        d.deviceName,
			DeviceType:        d.deviceType,
			HashRate:          averageHashRate,
			HashRateFormatted: util.FormatHashRate(averageHashRate),
			FanPercent:        fanPercent,
			Temperature:       temperature,
			Started:           d.started,
		})
	}

	w.Header().Add("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ms)
}
