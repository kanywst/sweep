//go:build linux

package proc

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// handle pins a process: on Linux a pidfd refers to the process that held the
// pid when it was opened, so a signal through it can never reach a later
// process that reused the pid between the identity check and the signal.
type handle struct{ fd int }

func open(pid int) *handle {
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil // old kernel or gone; fall back to kill(2)
	}
	return &handle{fd: fd}
}

func (h *handle) signal(pid int, sig syscall.Signal) error {
	if h == nil {
		return syscall.Kill(pid, sig)
	}
	return unix.PidfdSendSignal(h.fd, sig, nil, 0)
}

func (h *handle) close() {
	if h != nil {
		_ = unix.Close(h.fd)
	}
}
