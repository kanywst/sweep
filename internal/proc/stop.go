// Package proc stops processes.
package proc

import (
	"errors"
	"syscall"
	"time"
)

// Result is what happened to one pid.
type Result struct {
	PID    int
	Killed bool // needed SIGKILL after the grace period
	Err    error
}

// Stop sends SIGTERM to every pid, waits up to grace for them to exit, then
// sends SIGKILL to the ones still running.
func Stop(pids []int, grace time.Duration) []Result {
	res := make([]Result, len(pids))
	for i, pid := range pids {
		res[i].PID = pid
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			res[i].Err = err
		}
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !anyAlive(res) {
			return res
		}
		time.Sleep(100 * time.Millisecond)
	}
	for i := range res {
		if res[i].Err == nil && alive(res[i].PID) {
			res[i].Killed = true
			if err := syscall.Kill(res[i].PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				res[i].Err = err
			}
		}
	}
	return res
}

func anyAlive(res []Result) bool {
	for _, r := range res {
		if r.Err == nil && alive(r.PID) {
			return true
		}
	}
	return false
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
