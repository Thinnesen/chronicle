package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
)

func TestApproveTheWaitingNode(t *testing.T) {
	s := &stub{phase: "Running", gates: 1}
	out, err := run(t, s, "approve", "deploy")
	if err != nil || !strings.Contains(out, "Approved deploy.gate0") {
		t.Fatalf("one waiting node: %q %v", out, err)
	}
	if req := s.requests[0].(*chroniclev1.ApproveNodeRequest); req.Node != "deploy.gate0" {
		t.Errorf("node = %q", req.Node)
	}
	for gates, want := range map[int]string{0: "no step that waits", 2: "deploy.gate0, deploy.gate1"} {
		if _, err := run(t, &stub{phase: "Running", gates: gates}, "approve", "deploy"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%d waiting nodes: %v, want %q", gates, err, want)
		}
	}
}

func TestGetNamesTheApproveCommand(t *testing.T) {
	out, err := run(t, &stub{phase: "Running", gates: 1}, "get", "deploy")
	if err != nil || !strings.Contains(out, "chronicle approve deploy --namespace team-a") {
		t.Errorf("get: %q %v", out, err)
	}
}

func TestSubmitWithLog(t *testing.T) {
	file := writeWorkflow(t)
	out, err := run(t, &stub{phase: "Succeeded"}, "submit", "-f", file, "--log")
	if err != nil || !strings.Contains(out, "hello.a: hi") || !strings.Contains(out, "hello-abc Succeeded") {
		t.Fatalf("submit --log: %q %v", out, err)
	}
	if _, err := run(t, &stub{phase: "Failed"}, "submit", "-f", file, "--log"); !errors.Is(err, errRunFailed) {
		t.Errorf("a failed run: %v", err)
	}
}

func writeWorkflow(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "wf.yaml")
	if err := os.WriteFile(file, []byte("metadata: {generateName: hello-}\nspec: {entrypoint: c}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}
