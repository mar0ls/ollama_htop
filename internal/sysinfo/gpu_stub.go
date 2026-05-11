//go:build !linux

package sysinfo

type gpuReading struct { //nolint:unused
	available bool
	name      string  //nolint:unused
	percent   float64 //nolint:unused
	tempC     float64 //nolint:unused
	powerW    float64 //nolint:unused
}

func collectGPU() gpuReading { return gpuReading{} } //nolint:unused
