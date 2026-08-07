//go:build !windows

package hoststats

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// cpuSampler computes CPU busy% from successive cumulative /proc/stat
// snapshots. The kernel's counters are monotonic totals since boot, so a
// single read can't give a percentage -- only the delta between two reads
// separated by real time can. sampleOnce (its only caller) already runs on
// a fixed poll interval, so that interval doubles as the sampling window;
// no internal sleep needed.
type cpuSampler struct {
	prevTotal uint64
	prevIdle  uint64
	have      bool
}

func (c *cpuSampler) sample() float64 {
	total, idle, err := readProcStatCPU()
	if err != nil {
		return 0
	}
	if !c.have {
		c.prevTotal, c.prevIdle = total, idle
		c.have = true
		return 0
	}
	deltaTotal := total - c.prevTotal
	deltaIdle := idle - c.prevIdle
	c.prevTotal, c.prevIdle = total, idle
	if deltaTotal == 0 {
		return 0
	}
	pct := (1 - float64(deltaIdle)/float64(deltaTotal)) * 100
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	return pct
}

// readProcStatCPU parses the aggregate "cpu " line: user nice system idle
// iowait irq softirq steal guest guest_nice (guest/guest_nice already
// double-counted inside user/nice on Linux, per `man proc`, so they're not
// added again here). idle = idle + iowait, matching the convention `top`
// and most load calculators use.
func readProcStatCPU() (total, idle uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("hoststats: unexpected /proc/stat format")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, err
		}
		values = append(values, v)
		total += v
	}
	idle = values[3]
	if len(values) > 4 {
		idle += values[4] // iowait
	}
	return total, idle, nil
}
