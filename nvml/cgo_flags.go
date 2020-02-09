package nvml

/*
#cgo !windows LDFLAGS: -lnvidia-ml
#cgo windows LDFLAGS: -L../nvidia/NVSMI/ -lnvml
*/
import "C"
