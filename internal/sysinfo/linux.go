//go:build linux

package sysinfo

import (
	"bufio"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
)

type cpuCounters struct {
	user, nice, system, idle, iowait, irq, softirq uint64
}

var prevCPU cpuCounters

func readCPUCounters() (cpuCounters, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuCounters{}, err
	}
	defer f.Close() //nolint:errcheck

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			break
		}
		p := func(i int) uint64 {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			return v
		}
		return cpuCounters{
			user: p(1), nice: p(2), system: p(3), idle: p(4),
			iowait: p(5), irq: p(6), softirq: p(7),
		}, nil
	}
	return cpuCounters{}, nil
}

func cpuPercent() float64 {
	cur, err := readCPUCounters()
	if err != nil {
		return 0
	}
	prev := prevCPU
	prevCPU = cur

	idle := (cur.idle + cur.iowait) - (prev.idle + prev.iowait)
	total := (cur.user + cur.nice + cur.system + cur.idle + cur.iowait + cur.irq + cur.softirq) -
		(prev.user + prev.nice + prev.system + prev.idle + prev.iowait + prev.irq + prev.softirq)
	if total == 0 {
		return 0
	}
	return float64(total-idle) / float64(total) * 100
}

func memStats() (usedB, totalB uint64, pct float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck

	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		vals[key] = v * 1024
	}

	totalB = vals["MemTotal"]
	free := vals["MemFree"]
	buffers := vals["Buffers"]
	cached := vals["Cached"] + vals["SReclaimable"] - vals["Shmem"]
	usedB = totalB - free - buffers - cached
	if totalB > 0 {
		pct = float64(usedB) / float64(totalB) * 100
	}
	return
}

func loadAvg() (avg1, avg5, avg15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		avg1, _ = strconv.ParseFloat(fields[0], 64)
		avg5, _ = strconv.ParseFloat(fields[1], 64)
		avg15, _ = strconv.ParseFloat(fields[2], 64)
	}
	return
}

func cpuTempC() float64 {
	entries, err := os.ReadDir("/sys/class/thermal")
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "thermal_zone") {
			continue
		}
		typeData, err := os.ReadFile("/sys/class/thermal/" + e.Name() + "/type")
		if err != nil {
			continue
		}
		t := strings.TrimSpace(string(typeData))
		if !strings.Contains(t, "x86_pkg") && !strings.Contains(t, "cpu") && t != "acpitz" {
			continue
		}
		data, err := os.ReadFile("/sys/class/thermal/" + e.Name() + "/temp")
		if err != nil {
			continue
		}
		v, _ := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if v > 0 {
			return v / 1000
		}
	}
	return 0
}

// CollectStatic gathers one-time host metadata.
func CollectStatic() StaticInfo {
	si := StaticInfo{}

	if h, err := os.Hostname(); err == nil {
		si.Hostname = h
	}

	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil && ip.To4() != nil {
					si.IPAddress = ip.String()
					break
				}
			}
			if si.IPAddress != "" {
				break
			}
		}
	}

	if u, err := user.Current(); err == nil {
		si.Username = u.Username
	}

	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		defer f.Close() //nolint:errcheck
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					si.CPUName = strings.TrimSpace(parts[1])
					break
				}
			}
		}
	}

	if f, err := os.Open("/etc/os-release"); err == nil {
		defer f.Close() //nolint:errcheck
		sc := bufio.NewScanner(f)
		vals := map[string]string{}
		for sc.Scan() {
			parts := strings.SplitN(sc.Text(), "=", 2)
			if len(parts) == 2 {
				vals[parts[0]] = strings.Trim(parts[1], `"`)
			}
		}
		if name := vals["PRETTY_NAME"]; name != "" {
			si.OSVersion = name
		} else {
			si.OSVersion = vals["NAME"] + " " + vals["VERSION_ID"]
		}
	}

	return si
}

// Collect returns a fresh Info snapshot.
func Collect() Info {
	info := Info{}
	info.CPUPercent = cpuPercent()
	info.MemUsedB, info.MemTotalB, info.MemPercent = memStats()
	info.LoadAvg1, info.LoadAvg5, info.LoadAvg15 = loadAvg()
	info.CPUTempC = cpuTempC()
	if info.CPUTempC > 0 {
		info.SensorsAvail = true
	}

	g := collectGPU()
	info.GPUAvail = g.available
	info.GPUName = g.name
	if g.available {
		info.GPUPercent = g.percent
		info.GPUTempC = g.tempC
		info.GPUPowerW = g.powerW
		if g.tempC > 0 {
			info.SensorsAvail = true
		}
	}

	return info
}
