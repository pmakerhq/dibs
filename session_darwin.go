package main

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// processStartTime returns pid's start time (ms since epoch), used to
// detect PID reuse: a dead shell's PID can be recycled by the OS, but its
// start time won't match. A dead or unknown pid yields an error.
func processStartTime(pid int32) (int64, error) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if err != nil {
		return 0, fmt.Errorf("pid %d not found: %w", pid, err)
	}
	t := k.Proc.P_starttime
	return int64(t.Sec)*1000 + int64(t.Usec)/1000, nil
}
