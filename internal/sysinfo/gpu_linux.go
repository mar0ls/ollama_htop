//go:build linux

package sysinfo

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type gpuReading struct {
	available bool
	name      string
	percent   float64
	tempC     float64
	powerW    float64
}

func collectGPU() gpuReading {
	if g := nvidiaGPU(); g.available {
		return g
	}
	if g := amdGPU(); g.available {
		return g
	}
	return gpuReading{}
}

func nvidiaGPU() gpuReading {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return gpuReading{}
	}
	line := strings.TrimSpace(string(out))
	line = strings.SplitN(line, "\n", 2)[0]
	parts := strings.Split(line, ", ")
	if len(parts) < 5 {
		return gpuReading{}
	}
	parse := func(s string) float64 {
		s = strings.TrimSpace(s)
		if s == "[N/A]" || s == "N/A" {
			return 0
		}
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	g := gpuReading{
		available: true,
		name:      strings.TrimSpace(parts[0]),
		percent:   parse(parts[1]),
		tempC:     parse(parts[4]),
	}
	if len(parts) >= 6 {
		g.powerW = parse(parts[5])
	}
	return g
}

func amdGPU() gpuReading {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return gpuReading{}
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		base := "/sys/class/drm/" + name + "/device"
		busyData, err := os.ReadFile(base + "/gpu_busy_percent")
		if err != nil {
			continue
		}
		pct, _ := strconv.ParseFloat(strings.TrimSpace(string(busyData)), 64)
		return gpuReading{
			available: true,
			name:      "AMD GPU",
			percent:   pct,
			tempC:     amdTemp(base),
			powerW:    amdPower(base),
		}
	}
	return gpuReading{}
}

func amdTemp(deviceBase string) float64 {
	hwmonDir := deviceBase + "/hwmon"
	entries, err := os.ReadDir(hwmonDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		for _, sensor := range []string{"temp1_input", "temp2_input"} {
			data, err := os.ReadFile(hwmonDir + "/" + e.Name() + "/" + sensor)
			if err != nil {
				continue
			}
			v, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if v > 0 {
				return v / 1000
			}
		}
	}
	return 0
}

func amdPower(deviceBase string) float64 {
	hwmonDir := deviceBase + "/hwmon"
	entries, err := os.ReadDir(hwmonDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		data, err := os.ReadFile(hwmonDir + "/" + e.Name() + "/power1_input")
		if err != nil {
			continue
		}
		v, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if v > 0 {
			return v / 1_000_000
		}
	}
	return 0
}
