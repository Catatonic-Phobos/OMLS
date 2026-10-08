//go:build !linux || !cgo

package gpu

import (
	"errors"
	"time"
)

type Device struct {
	Index       int
	Name        string
	Vendor      string
	VendorID    uint32
	ComputeUnit uint32
}

func Devices() ([]Device, error) {
	return nil, errors.New("GPU compute requires Linux, cgo, and an OpenCL runtime")
}
func Burn(int, int64) (time.Duration, error) {
	return 0, errors.New("GPU compute requires Linux, cgo, and an OpenCL runtime")
}
