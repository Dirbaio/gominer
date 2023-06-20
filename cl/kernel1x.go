package cl

/*
#include "cl.h"
*/
import "C"

func CLEnqueueTask(command_queue CL_command_queue,
	kernel CL_kernel,
	num_events_in_wait_list CL_uint,
	event_wait_list []CL_event,
	event *CL_event) CL_int {

	if (num_events_in_wait_list == 0 && event_wait_list != nil) ||
		(num_events_in_wait_list != 0 && event_wait_list == nil) ||
		int(num_events_in_wait_list) != len(event_wait_list) {
		return CL_INVALID_EVENT_WAIT_LIST
	}

	var c_event C.cl_event
	var c_errcode_ret C.cl_int

	if num_events_in_wait_list != 0 {
		c_event_wait_list := make([]C.cl_event, num_events_in_wait_list)
		for i := 0; i < int(num_events_in_wait_list); i++ {
			c_event_wait_list[i] = event_wait_list[i].cl_event
		}

		c_errcode_ret = C.clEnqueueTask(command_queue.cl_command_queue,
			kernel.cl_kernel,
			C.cl_uint(num_events_in_wait_list),
			&c_event_wait_list[0],
			&c_event)
	} else {
		c_errcode_ret = C.clEnqueueTask(command_queue.cl_command_queue,
			kernel.cl_kernel,
			0,
			nil,
			&c_event)
	}

	if event != nil {
		event.cl_event = c_event
	}

	return CL_int(c_errcode_ret)
}
