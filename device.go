// Copyright (c) 2016 The Decred developers.

package main

import (
	"encoding/binary"
	"encoding/hex"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"

	"github.com/decred/gominer/blake256"
	"github.com/decred/gominer/util"
	"github.com/decred/gominer/work"
)

var chainParams = &chaincfg.MainNetParams

func (d *Device) updateCurrentWork() {
	var w *work.Work
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
	minrLog.Tracef("pre-nonce: %v", hex.EncodeToString(d.work.Data[:]))

	// Bump and set the work ID if the work is new.
	d.currentWorkID++
	binary.LittleEndian.PutUint32(d.work.Data[128+4*work.Nonce2Word:],
		d.currentWorkID)

	// Reset the hash state
	copy(d.midstate[:], blake256.IV256[:])

	// Hash the two first blocks
	blake256.Block(d.midstate[:], d.work.Data[0:64], 512)
	blake256.Block(d.midstate[:], d.work.Data[64:128], 1024)
	minrLog.Tracef("midstate input data for work update %v",
		hex.EncodeToString(d.work.Data[0:128]))

	// Convert the next block to uint32 array.
	for i := 0; i < 16; i++ {
		d.lastBlock[i] = binary.BigEndian.Uint32(d.work.Data[128+i*4 : 132+i*4])
	}
	minrLog.Tracef("work data for work update: %v",
		hex.EncodeToString(d.work.Data[:]))
}

func (d *Device) Run() {
	err := d.runDevice()
	if err != nil {
		minrLog.Errorf("Error on device: %v", err)
	}
}

func (d *Device) foundCandidate(ts, nonce0, nonce1 uint32) {
	d.Lock()
	defer d.Unlock()
	// Construct the final block header.
	data := make([]byte, 192)
	copy(data, d.work.Data[:])

	binary.BigEndian.PutUint32(data[128+4*work.TimestampWord:], ts)
	binary.BigEndian.PutUint32(data[128+4*work.Nonce0Word:], nonce0)
	binary.BigEndian.PutUint32(data[128+4*work.Nonce1Word:], nonce1)
	hash := chainhash.HashFuncH(data[0:180])

	// Hashes that reach this logic and fail the minimal proof of
	// work check are considered to be hardware errors.
	hashNum := blockchain.ShaHashToBig(&hash)
	if hashNum.Cmp(chainParams.PowLimit) > 0 {
		minrLog.Errorf("DEV #%d: Hardware error found, hash %v above "+
			"minimum target %064x", d.index, hash, d.work.Target.Bytes())
		d.invalidShares++
		return
	}

	d.allDiffOneShares++

	if !cfg.Benchmark {
		// Assess versus the pool or daemon target.
		if hashNum.Cmp(d.work.Target) > 0 {
			minrLog.Debugf("DEV #%d: Hash %v bigger than target %032x (boo)",
				d.index, hash, d.work.Target.Bytes())
		} else {
			minrLog.Infof("DEV #%d: Found hash with work below target! %v (yay)",
				d.index, hash)
			d.validShares++
			d.workDone <- data
		}
	}
}

func (d *Device) Stop() {
	close(d.quit)
}

func (d *Device) SetWork(w *work.Work) {
	d.newWork <- w
}

func (d *Device) PrintStats() {
	secondsElapsed := uint32(time.Now().Unix()) - d.started
	if secondsElapsed == 0 {
		return
	}

	diffOneShareHashesAvg := uint64(0x00000000FFFFFFFF)
	d.Lock()
	defer d.Unlock()
	averageHashRate := (float64(diffOneShareHashesAvg) *
		float64(d.allDiffOneShares)) /
		float64(secondsElapsed)

	fanPercent := atomic.LoadUint32(&d.fanPercent)
	temperature := atomic.LoadUint32(&d.temperature)

	if fanPercent != 0 || temperature != 0 {
		minrLog.Infof("DEV #%d (%s) reporting average hash rate %v, %v/%v valid work, Fan=%v%% Temp=%vC",
			d.index,
			d.deviceName,
			util.FormatHashRate(averageHashRate),
			d.validShares,
			d.validShares+d.invalidShares,
			fanPercent,
			temperature)
	} else {
		minrLog.Infof("DEV #%d (%s) reporting average hash rate %v, %v/%v valid work",
			d.index,
			d.deviceName,
			util.FormatHashRate(averageHashRate),
			d.validShares,
			d.validShares+d.invalidShares)
	}
}

// UpdateFanTemp updates a device's statistics
func (d *Device) UpdateFanTemp() {
	d.Lock()
	defer d.Unlock()
	if d.fanTempActive {
		// For now amd and nvidia do more or less the same thing
		// but could be split up later.  Anything else (Intel) just
		// don't do anything.
		switch d.kind {
		case "adl", "amdgpu", "nvidia":
			fanPercent, temperature := deviceStats(d.index)
			atomic.StoreUint32(&d.fanPercent, fanPercent)
			atomic.StoreUint32(&d.temperature, temperature)
			break
		}
	}
}
