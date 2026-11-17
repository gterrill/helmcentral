//go:build unix

package main

import (
	"syscall"
	"time"
)

// processCPUTime is the user plus system CPU time this process has used,
// from the operating system (getrusage), so it counts every thread and does
// not wait on the Go runtime's own accounting.
func processCPUTime() (time.Duration, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, err
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano()), nil
}
