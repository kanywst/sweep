package proc

import (
	"os/exec"
	"testing"
	"time"
)

func TestStopTerminates(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()

	res := Stop([]int{cmd.Process.Pid}, 2*time.Second)
	if res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process still running")
	}
}

func TestStopEscalatesToKill(t *testing.T) {
	cmd := exec.Command("sh", "-c", `trap "" TERM; sleep 60`)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	time.Sleep(200 * time.Millisecond) // let the trap install

	res := Stop([]int{cmd.Process.Pid}, 300*time.Millisecond)
	if !res[0].Killed {
		t.Errorf("expected SIGKILL escalation, got %+v", res[0])
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("process still running")
	}
}

func TestStopGonePID(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	res := Stop([]int{cmd.Process.Pid}, 100*time.Millisecond)
	if res[0].Err != nil {
		t.Fatalf("gone pid should not be an error: %v", res[0].Err)
	}
}
