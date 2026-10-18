//go:build !linux

package proc

import "syscall"

// handle is a no-op where pidfds do not exist; the start-time check right
// before the signal is what guards against a reused pid.
type handle struct{}

func open(int) *handle { return nil }

func (*handle) signal(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }

func (*handle) close() {}
