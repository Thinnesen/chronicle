package cli

import (
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
