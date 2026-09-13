package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// clockTicks is the kernel's USER_HZ, i.e. the unit of the starttime field
// in /proc/<pid>/stat. It is 100 on every mainstream Linux build, and the
// C library's sysconf(_SC_CLK_TCK) isn't reachable without cgo.
const clockTicks = 100

// processStartTime returns pid's start time (ms since epoch), used to
// detect PID reuse: a dead shell's PID can be recycled by the OS, but its
// start time won't match. A dead or unknown pid yields an error.
func processStartTime(pid int32) (int64, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, fmt.Errorf("pid %d not found: %w", pid, err)
	}
	// Field 2 is the executable name in parentheses and may itself contain
	// spaces or parentheses, so fields are counted from the last ')'.
	close := strings.LastIndexByte(string(stat), ')')
	if close < 0 {
		return 0, fmt.Errorf("pid %d: malformed /proc stat", pid)
	}
	fields := strings.Fields(string(stat)[close+1:])
	// fields[0] is field 3 (state), so field 22 (starttime) is fields[19].
	if len(fields) < 20 {
		return 0, fmt.Errorf("pid %d: malformed /proc stat", pid)
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("pid %d: parse starttime: %w", pid, err)
	}
	boot, err := bootTime()
	if err != nil {
		return 0, err
	}
	return int64(ticks/clockTicks+boot) * 1000, nil
}

// bootTime returns the kernel's boot time in seconds since the epoch, which
// /proc/<pid>/stat's starttime is relative to.
func bootTime() (uint64, error) {
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(stat), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		}
	}
	return 0, fmt.Errorf("btime not found in /proc/stat")
}
