package executor

import (
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestRunChildKillsAfterGrace stops a command that ignores SIGTERM, as a shell that waits for "sleep" does. The executor
// must end it in time, so that it can still save the log before Kubernetes kills the Pod.
func TestRunChildKillsAfterGrace(t *testing.T) {
	old := killGrace
	killGrace = 200 * time.Millisecond
	t.Cleanup(func() { killGrace = old })
	cmd := exec.Command("sh", "-c", `trap "" TERM; sleep 30`)
	cmd.Stdout = io.Discard
	done := make(chan int)
	go func() { done <- runChild(cmd, io.Discard) }()
	time.Sleep(300 * time.Millisecond) // the child runs and has its trap
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 128+int(syscall.SIGKILL) {
			t.Errorf("exit code %d, want %d", code, 128+int(syscall.SIGKILL))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the executor still waits for the command")
	}
}
