package cl

/*
#include "cl.h"
*/
import "C"

func CLCreateCommandQueue(context CL_context,
	device CL_device_id,
	properties CL_command_queue_properties,
	errcode_ret *CL_int) CL_command_queue {
	var c_errcode_ret C.cl_int
	var c_command_queue C.cl_command_queue

	c_command_queue = C.clCreateCommandQueue(context.cl_context,
		device.cl_device_id,
		C.cl_command_queue_properties(properties),
		&c_errcode_ret)

	if errcode_ret != nil {
		*errcode_ret = CL_int(c_errcode_ret)
	}

	return CL_command_queue{c_command_queue}
}
