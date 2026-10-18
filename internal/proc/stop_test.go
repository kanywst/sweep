package proc

import (
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/kanywst/sweep/internal/ps"
)

func start(t *testing.T, args ...string) (*exec.Cmd, Target, chan struct{}) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	time.Sleep(200 * time.Millisecond) // let ps see it and any trap install
	cur, err := SystemLookup([]int{cmd.Process.Pid})
	if err != nil {
		t.Fatal(err)
	}
	return cmd, Target{PID: cmd.Process.Pid, Started: cur[cmd.Process.Pid].Started}, done
}

func waitDone(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process still running")
	}
}

func TestStopTerminates(t *testing.T) {
	_, tg, done := start(t, "sleep", "60")
	res := Stop([]Target{tg}, 2*time.Second, SystemLookup)
	if res[0].Err != nil || res[0].Killed {
		t.Fatalf("%+v", res[0])
	}
	waitDone(t, done)
}

func TestStopEscalatesToKill(t *testing.T) {
	_, tg, done := start(t, "sh", "-c", `trap "" TERM; sleep 60`)
	res := Stop([]Target{tg}, 300*time.Millisecond, SystemLookup)
	if !res[0].Killed {
		t.Errorf("expected SIGKILL escalation, got %+v", res[0])
	}
	waitDone(t, done)
}

func TestStopGonePID(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	res := Stop([]Target{{PID: cmd.Process.Pid}}, 100*time.Millisecond, SystemLookup)
	if res[0].Err != nil || !res[0].Gone {
		t.Fatalf("an exited pid is gone, not an error: %+v", res[0])
	}
}

func TestStopSkipsRecycledPID(t *testing.T) {
	cmd, tg, done := start(t, "sleep", "60")
	defer func() { _ = cmd.Process.Kill(); <-done }()
	tg.Started = tg.Started.Add(-time.Hour) // the scan saw an older process with this pid
	res := Stop([]Target{tg}, 100*time.Millisecond, SystemLookup)
	if !errors.Is(res[0].Err, ErrChanged) {
		t.Fatalf("got %+v, want ErrChanged", res[0])
	}
	select {
	case <-done:
		t.Fatal("a recycled pid was signalled")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestStopRefusesLowPIDs(t *testing.T) {
	res := Stop([]Target{{PID: 0}, {PID: 1}, {PID: -1}}, 0, func([]int) (map[int]ps.Proc, error) {
		return map[int]ps.Proc{1: {PID: 1}}, nil
	})
	for _, r := range res {
		if !errors.Is(r.Err, ErrRefused) {
			t.Errorf("pid %d: %+v", r.PID, r)
		}
	}
}

func TestStopTreatsZombieAsExited(t *testing.T) {
	calls := 0
	lookup := func([]int) (map[int]ps.Proc, error) {
		calls++
		stat := "S"
		if calls > 1 {
			stat = "Z" // exited after SIGTERM but not yet reaped
		}
		return map[int]ps.Proc{424242: {PID: 424242, Stat: stat}}, nil
	}
	start := time.Now()
	res := Stop([]Target{{PID: 424242}}, 2*time.Second, lookup)
	if res[0].Killed || time.Since(start) > time.Second {
		t.Fatalf("zombie should count as stopped right away: %+v after %v", res[0], time.Since(start))
	}
}
