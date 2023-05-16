package cl

/*
#include "cl.h"
*/
import "C"

func CLCreateSampler(context CL_context,
	normalized_coords CL_bool,
	addressing_mode CL_addressing_mode,
	filter_mode CL_filter_mode,
	errcode_ret *CL_int) CL_sampler {

	var c_errcode_ret C.cl_int

	c_sampler := C.clCreateSampler(context.cl_context,
		C.cl_bool(normalized_coords),
		C.cl_addressing_mode(addressing_mode),
		C.cl_filter_mode(filter_mode),
		&c_errcode_ret)

	if errcode_ret != nil {
		*errcode_ret = CL_int(c_errcode_ret)
	}

	return CL_sampler{c_sampler}
}
