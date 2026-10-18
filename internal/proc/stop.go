// Package proc stops processes.
package proc

import (
	"context"
	"errors"
	"syscall"
	"time"

	"github.com/kanywst/sweep/internal/ps"
)

// Target is a process as it was when it was scanned.
type Target struct {
	PID     int
	Started time.Time
}

// Result is what happened to one target.
type Result struct {
	PID    int
	Killed bool // needed SIGKILL after the grace period
	Gone   bool // had already exited before it was signalled
	Err    error
}

var (
	// ErrChanged means the pid now belongs to a different process than the
	// one that was scanned, so it was left alone.
	ErrChanged = errors.New("pid was reused by another process")
	// ErrRefused is returned for pids that are never signalled (0, 1, negative).
	ErrRefused = errors.New("refusing to signal this pid")
)

// Lookup returns the current process table entries for pids. Missing pids
// have exited.
type Lookup func(pids []int) (map[int]ps.Proc, error)

// SystemLookup reads the live process table.
func SystemLookup(pids []int) (map[int]ps.Proc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ps.Lookup(ctx, ps.Exec, pids)
}

// Stop sends SIGTERM to every target, waits up to grace for them to exit,
// then sends SIGKILL to the ones still running. Before each signal it checks
// the pid still belongs to the scanned process (same start time), so a pid
// recycled since the scan is never signalled. Zombies count as exited.
func Stop(targets []Target, grace time.Duration, lookup Lookup) []Result {
	res := make([]Result, len(targets))
	pids := make([]int, len(targets))
	hs := make([]*handle, len(targets))
	for i, t := range targets {
		res[i].PID, pids[i] = t.PID, t.PID
		if t.PID > 1 {
			hs[i] = open(t.PID) // before the check, so the check covers what we signal
		}
	}
	defer func() {
		for _, h := range hs {
			h.close()
		}
	}()

	cur, err := lookup(pids)
	if err != nil {
		for i := range res {
			res[i].Err = err
		}
		return res
	}
	for i, t := range targets {
		switch state(t, cur) {
		case refused:
			res[i].Err = ErrRefused
		case changed:
			res[i].Err = ErrChanged
		case exited:
			res[i].Gone = true
		case running:
			if err := hs[i].signal(t.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				res[i].Err = err
			}
		}
	}

	deadline := time.Now().Add(grace)
	for {
		cur, err = lookup(pids)
		if err != nil {
			return res
		}
		pending := false
		for i, t := range targets {
			if res[i].Err == nil && !res[i].Gone && state(t, cur) == running {
				pending = true
			}
		}
		if !pending || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	for i, t := range targets {
		if res[i].Err != nil || res[i].Gone || state(t, cur) != running {
			continue
		}
		res[i].Killed = true
		if err := hs[i].signal(t.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			res[i].Err = err
		}
	}
	return res
}

type procState int

const (
	running procState = iota
	exited
	changed
	refused
)

func state(t Target, cur map[int]ps.Proc) procState {
	if t.PID <= 1 {
		return refused
	}
	p, ok := cur[t.PID]
	switch {
	case !ok || p.Zombie():
		return exited
	case !t.Started.IsZero() && !p.Same(ps.Proc{Started: t.Started}):
		return changed
	}
	return running
}
