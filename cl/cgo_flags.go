package cl

/*
#cgo CFLAGS: -I CL
#cgo windows CFLAGS: -IC:/appsdk/include
#cgo !darwin LDFLAGS: -lOpenCL
#cgo darwin LDFLAGS: -framework OpenCL
#cgo windows LDFLAGS: -LC:/appsdk/lib/x86_64
*/
import "C"
