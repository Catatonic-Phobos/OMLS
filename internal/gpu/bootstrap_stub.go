//go:build !linux || !cgo

package gpu

func PrepareOpenCL() error { return nil }
