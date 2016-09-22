package adl

/*
// XXX all the C implementations use dlopen()
#cgo linux CFLAGS: -DLINUX
#cgo linux LDFLAGS: -latiadlxx -ldl
#include <stddef.h>
#include <stdbool.h>
#include <adl_sdk.h>
int getADLFanPercent(int deviceid);
int getADLTemp(int deviceid);
*/
import "C"

// DeviceFanPercent fetches and returns fan utilization for a device index
func DeviceFanPercent(index int) uint32 {
	fanPercent := uint32(0)

	fanPercent = uint32(C.getADLFanPercent(C.int(index)))

	return fanPercent
}

// DeviceTemperature fetches and returns temperature for a device index
func DeviceTemperature(index int) uint32 {
	temperature := uint32(0)

	temperature = uint32(C.getADLTemp(C.int(index)))

	return temperature
}
