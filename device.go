// Copyright (c) 2016-2023 The Decred developers.

package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/gominer/blake3"
	"github.com/decred/gominer/util"
	"github.com/decred/gominer/work"
)

var chainParams = chaincfg.MainNetParams()
var deviceLibraryInitialized = false // nolint:unused

// randDeviceOffset1 and randDeviceOffset2 are random offsets to use for all
// devices so each process ends up with a random starting point for all devices.
var randDeviceOffset1, randDeviceOffset2 uint8

func init() {
	var buf [2]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		panic(err)
	}
	randDeviceOffset1 = buf[0]
	randDeviceOffset2 = buf[1]
}

// Constants for fan and temperature bits.
const (
	ADLFanFailSafe            = uint32(80)
	AMDGPUFanFailSafe         = uint32(204)
	AMDGPUFanMax              = uint32(255)
	AMDTempDivisor            = uint32(1000)
	ChangeLevelNone           = "None"
	ChangeLevelSmall          = "Small"
	ChangeLevelLarge          = "Large"
	DeviceKindAMDGPU          = "AMDGPU"
	DeviceKindADL             = "ADL"
	DeviceKindNVML            = "NVML"
	DeviceKindUnknown         = "Unknown"
	DeviceTypeCPU             = "CPU"
	DeviceTypeGPU             = "GPU"
	FanControlHysteresis      = uint32(3)
	FanControlAdjustmentLarge = uint32(10)
	FanControlAdjustmentSmall = uint32(5)
	SeverityLow               = "Low"
	SeverityHigh              = "High"
	TargetLower               = "Lower"
	TargetHigher              = "Raise"
	TargetNone                = "None"
)

// initNonces initialize the nonces for the device such that each device in the
// same system is doing different work while also helping prevent collisions
// across multiple processes and systems working on the same template.
func (d *Device) initNonces() error {
	// Read cryptographically random data for use below in setting the initial
	// nonces.
	var buf [8]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return fmt.Errorf("unable to read random value: %w", err)
	}
	extraNonceRandOffset := binary.LittleEndian.Uint32(buf[0:])
	extraNonce2RandOffset := binary.LittleEndian.Uint32(buf[4:])

	// Set the initial extra nonce as follows:
	// - The first byte is the device ID offset by the first per-process random
	//   device offset
	// - The remaining 3 bytes are a per-device random extra nonce offset
	//
	// This, when coupled with the second per-process random device offset set
	// elsewhere, ensures each device in the same system is doing different work
	// (up to 65536 devices) while also helping prevent collisions across
	// multiple processes and systems working on the same template.
	deviceOffset := (uint32(d.index) + uint32(randDeviceOffset1)) % 255
	d.extraNonce = deviceOffset<<24 | extraNonceRandOffset&0x00ffffff

	// Set the current work ID to a random initial value.
	//
	// The current work ID is also treated as a secondary extra nonce and thus,
	// when combined with the extra nonce above, the result is that the pair
	// effectively acts as an 8-byte randomized extra nonce.
	d.currentWorkID = extraNonce2RandOffset

	minrLog.Debugf("DEV #%d: initial extraNonce %x, initial workID: %x",
		d.index, d.extraNonce, d.currentWorkID)
	return nil
}

func (d *Device) updateCurrentWork(ctx context.Context) {
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
		// If we don't have work, we block until we do.
		select {
		case w = <-d.newWork:
		case <-ctx.Done():
			return
		}
	}

	d.hasWork = true

	d.work = *w
	minrLog.Tracef("pre-nonce: %x", d.work.Data[:])

	// Bump and set the work ID.
	d.currentWorkID++
	binary.LittleEndian.PutUint32(d.work.Data[128+4*work.Nonce2Word:],
		d.currentWorkID)

	// Set additional byte with the device id offset by a second per-process
	// random device offset to support up to 65536 devices.
	deviceID := uint8((uint32(d.index) + uint32(randDeviceOffset2)) % 255)
	d.work.Data[128+4*work.Nonce3Word] = deviceID

	// Hash the two first blocks.
	d.midstate = blake3.Block(blake3.IV, d.work.Data[0:64], blake3.FlagChunkStart)
	d.midstate = blake3.Block(d.midstate, d.work.Data[64:128], 0)
	minrLog.Tracef("midstate input data for work update %x", d.work.Data[0:128])

	// Convert the next block to uint32 array.
	for i := 0; i < 16; i++ {
		d.lastBlock[i] = binary.LittleEndian.Uint32(d.work.Data[128+i*4:])
	}
	minrLog.Tracef("work data for work update: %x", d.work.Data)
}

func (d *Device) Run(ctx context.Context) {
	err := d.runDevice(ctx)
	if err != nil {
		minrLog.Errorf("Error on device: %v", err)
	}
}

// This is pretty hacky/proof-of-concepty.
func (d *Device) fanControl() {
	d.Lock()
	defer d.Unlock()
	var fanChangeLevel, fanIntent string
	var fanChange uint32
	fanLast := d.fanControlLastFanPercent

	var tempChange uint32
	var tempChangeLevel, tempDirection string
	var tempSeverity, tempTargetType string

	var firstRun bool

	tempLast := d.fanControlLastTemp
	tempMinAllowed := d.tempTarget - FanControlHysteresis
	tempMaxAllowed := d.tempTarget + FanControlHysteresis

	// Save the values we read for the next time the loop is run
	fanCur := atomic.LoadUint32(&d.fanPercent)
	tempCur := atomic.LoadUint32(&d.temperature)
	d.fanControlLastFanPercent = fanCur
	d.fanControlLastTemp = tempCur

	// if this is our first run then set some more variables
	if tempLast == 0 && fanLast == 0 {
		fanLast = fanCur
		tempLast = tempCur
		firstRun = true
	}

	// Everything is OK so just return without adjustment
	if tempCur <= tempMaxAllowed && tempCur >= tempMinAllowed {
		minrLog.Tracef("DEV #%d within acceptable limits "+
			"curTemp %v is above minimum %v and below maximum %v",
			d.index, tempCur, tempMinAllowed, tempMaxAllowed)
		return
	}

	// Lower the temperature of the device
	if tempCur > tempMaxAllowed {
		tempTargetType = TargetLower
		if tempCur-tempMaxAllowed > FanControlHysteresis {
			tempSeverity = SeverityHigh
		} else {
			tempSeverity = SeverityLow
		}
	}

	// Raise the temperature of the device
	if tempCur < tempMinAllowed {
		tempTargetType = TargetHigher
		if tempMaxAllowed-tempCur >= FanControlHysteresis {
			tempSeverity = SeverityHigh
		} else {
			tempSeverity = SeverityLow
		}
	}

	// we increased the fan to lower the device temperature last time
	if fanLast < fanCur {
		fanChange = fanCur - fanLast
		fanIntent = TargetHigher
	}
	// we decreased the fan to raise the device temperature last time
	if fanLast > fanCur {
		fanChange = fanLast - fanCur
		fanIntent = TargetLower
	}
	// we didn't make any changes
	if fanLast == fanCur {
		fanIntent = TargetNone
	}

	if fanChange == 0 {
		fanChangeLevel = ChangeLevelNone
	} else if fanChange == FanControlAdjustmentSmall {
		fanChangeLevel = ChangeLevelSmall
	} else if fanChange == FanControlAdjustmentLarge {
		fanChangeLevel = ChangeLevelLarge
	} else {
		// XXX Seems the AMDGPU driver may not support all values or
		// changes values underneath us
		minrLog.Tracef("DEV #%d fan changed by an unexpected value %v", d.index,
			fanChange)
		if fanChange < FanControlAdjustmentSmall {
			fanChangeLevel = ChangeLevelSmall
		} else {
			fanChangeLevel = ChangeLevelLarge
		}
	}

	if tempLast < tempCur {
		tempChange = tempCur - tempLast
		tempDirection = "Up"
	}
	if tempLast > tempCur {
		tempChange = tempLast - tempCur
		tempDirection = "Down"
	}
	if tempLast == tempCur {
		tempDirection = "Stable"
	}

	if tempChange == 0 {
		tempChangeLevel = ChangeLevelNone
	} else if tempChange > FanControlHysteresis {
		tempChangeLevel = ChangeLevelLarge
	} else {
		tempChangeLevel = ChangeLevelSmall
	}

	minrLog.Tracef("DEV #%d firstRun %v fanChange %v fanChangeLevel %v "+
		"fanIntent %v tempChange %v tempChangeLevel %v tempDirection %v "+
		" tempSeverity %v tempTargetType %v", d.index, firstRun, fanChange,
		fanChangeLevel, fanIntent, tempChange, tempChangeLevel, tempDirection,
		tempSeverity, tempTargetType)

	// We have no idea if the device is starting cold or re-starting hot
	// so only adjust the fans upwards a little bit.
	if firstRun {
		if tempTargetType == TargetLower {
			fanControlSet(d.index, fanCur, tempTargetType, ChangeLevelSmall)
			return
		}
	}

	// we didn't do anything last time so just match our change to the severity
	if fanIntent == TargetNone {
		if tempSeverity == SeverityLow {
			fanControlSet(d.index, fanCur, tempTargetType, ChangeLevelSmall)
		} else {
			fanControlSet(d.index, fanCur, tempTargetType, ChangeLevelLarge)
		}
	}

	// XXX could do some more hysteresis stuff here

	// we tried to raise or lower the temperature but it didn't work so
	// do it some more according to the severity level
	if fanIntent == tempTargetType {
		if tempSeverity == SeverityLow {
			fanControlSet(d.index, fanCur, tempTargetType, ChangeLevelSmall)
		} else {
			fanControlSet(d.index, fanCur, tempTargetType, ChangeLevelLarge)
		}
	}

	// we raised or lowered the temperature too much so just do a small
	// adjustment
	if fanIntent != tempTargetType {
		fanControlSet(d.index, fanCur, tempTargetType, ChangeLevelSmall)
	}
}

func (d *Device) fanControlSupported(kind string) bool {
	fanControlDrivers := []string{DeviceKindADL, DeviceKindAMDGPU}

	for _, driver := range fanControlDrivers {
		if driver == kind {
			return true
		}
	}
	return false
}

func (d *Device) foundCandidate(ts, nonce0, nonce1 uint32) {
	d.Lock()
	defer d.Unlock()
	// Construct the final block header.
	data := make([]byte, 192)
	copy(data, d.work.Data[:])

	binary.LittleEndian.PutUint32(data[128+4*work.TimestampWord:], ts)
	binary.LittleEndian.PutUint32(data[128+4*work.Nonce0Word:], nonce0)
	binary.LittleEndian.PutUint32(data[128+4*work.Nonce1Word:], nonce1)
	hash := chainhash.Hash(blake3.FinalBlock(d.midstate, data[128:180]))

	// Hashes that reach this logic and fail the minimal proof of
	// work check are considered to be hardware errors.
	hashNum := standalone.HashToBig(&hash)
	if hashNum.Cmp(chainParams.PowLimit) > 0 {
		minrLog.Errorf("DEV #%d: Hardware error found, hash %v above "+
			"minimum target %064x", d.index, hash, chainParams.PowLimit)
		d.invalidShares++
		return
	}

	d.allDiffOneShares++

	if !cfg.Benchmark {
		// Assess versus the pool or daemon target.
		if hashNum.Cmp(d.work.Target) > 0 {
			minrLog.Debugf("DEV #%d: Hash %v bigger than target %064x (boo)",
				d.index, hash, d.work.Target)
		} else {
			minrLog.Infof("DEV #%d: Found hash with work below target! %v (yay)",
				d.index, hash)
			d.validShares++
			d.workDone <- data
		}
	}
}

func (d *Device) SetWork(ctx context.Context, w *work.Work) {
	select {
	case d.newWork <- w:
	case <-ctx.Done():
	}
}

func (d *Device) PrintStats() {
	secondsElapsed := uint32(time.Now().Unix()) - d.started
	if secondsElapsed == 0 {
		return
	}

	d.Lock()
	defer d.Unlock()

	averageHashRate, fanPercent, temperature := d.Status()

	if fanPercent != 0 || temperature != 0 {
		minrLog.Infof("DEV #%d (%s) %v Fan=%v%% T=%vC",
			d.index,
			d.deviceName,
			util.FormatHashRate(averageHashRate),
			fanPercent,
			temperature)
	} else {
		minrLog.Infof("DEV #%d (%s) %v",
			d.index,
			d.deviceName,
			util.FormatHashRate(averageHashRate),
		)
	}
}

// UpdateFanTemp updates a device's statistics.
func (d *Device) UpdateFanTemp() {
	d.Lock()
	defer d.Unlock()
	if d.fanTempActive {
		// For now amd and nvidia do more or less the same thing
		// but could be split up later.  Anything else (Intel) just
		// doesn't do anything.
		switch d.kind {
		case DeviceKindADL, DeviceKindAMDGPU, DeviceKindNVML:
			fanPercent, temperature := deviceStats(d.index)
			atomic.StoreUint32(&d.fanPercent, fanPercent)
			atomic.StoreUint32(&d.temperature, temperature)
		}
	}
}

func (d *Device) Status() (float64, uint32, uint32) {
	secondsElapsed := uint32(time.Now().Unix()) - d.started
	diffOneShareHashesAvg := uint64(0x00000000FFFFFFFF)

	averageHashRate := (float64(diffOneShareHashesAvg) *
		float64(d.allDiffOneShares)) /
		float64(secondsElapsed)

	fanPercent := atomic.LoadUint32(&d.fanPercent)
	temperature := atomic.LoadUint32(&d.temperature)

	return averageHashRate, fanPercent, temperature
}
