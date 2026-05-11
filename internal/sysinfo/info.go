// Package sysinfo collects host resource metrics (CPU, RAM, GPU, load averages).
package sysinfo

// StaticInfo holds data gathered once at startup.
type StaticInfo struct {
	Hostname  string
	IPAddress string
	Username  string
	CPUName   string
	OSVersion string
}

// Info is a point-in-time snapshot of host resource usage.
type Info struct {
	CPUPercent float64
	GPUPercent float64
	GPUAvail   bool
	GPUName    string
	MemUsedB   uint64
	MemTotalB  uint64
	MemPercent float64
	CPUTempC   float64
	GPUTempC   float64
	LoadAvg1   float64
	LoadAvg5   float64
	LoadAvg15  float64

	SensorsAvail   bool
	CPUTempHistory []float64
	GPUTempHistory []float64
	ActiveBuckets  int

	GPUPowerW  float64
	TokPerWatt float64

	// Merged from StaticInfo by the store on each snapshot.
	Hostname  string
	IPAddress string
	Username  string
	CPUName   string
	OSVersion string
}
