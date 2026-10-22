package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

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

func TestErrorText(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{connect.NewError(connect.CodePermissionDenied, errors.New("workflows is forbidden")), "workflows is forbidden"},
		{connect.NewError(connect.CodeUnauthenticated, errors.New("the token is not valid")), "the token is not valid. Log in again: chronicle login"},
		{errors.New("plain"), "plain"},
	}
	for _, tt := range tests {
		if got := errorText(tt.err); got != tt.want {
			t.Errorf("errorText(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestListWithoutTheArchive(t *testing.T) {
	out, err := run(t, &stub{archiveDown: true}, "list")
	if err != nil || !strings.Contains(out, "run-1") || !strings.Contains(out, "The archive did not answer") {
		t.Errorf("list: %q %v", out, err)
	}
}

func TestGetShowsTheEarlierTries(t *testing.T) {
	st := &stub{phase: "Succeeded", tries: []*chroniclev1.Try{{Number: 0, Phase: "Failed"}}}
	out, err := run(t, st, "get", "release")
	if err != nil || !strings.Contains(out, "Tries:     1 Failed, latest (shown). Show a try with --try <number>.") {
		t.Errorf("get: %q %v", out, err)
	}
	// The numbers start at 1, as in the UI. The API counts from 0.
	out, err = run(t, st, "get", "release", "--try", "1")
	if err != nil || !strings.Contains(out, "Phase:     Failed in try 0") || !strings.Contains(out, "Tries:     1 Failed (shown), latest.") {
		t.Errorf("get --try: %q %v", out, err)
	}
}

func TestGetShows(t *testing.T) {
	at := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		stub *stub
		want []string
	}{
		{"the approve command", &stub{phase: "Running", gates: 1}, []string{"chronicle approve release --namespace team-a"}},
		{"the outputs", &stub{phase: "Succeeded"}, []string{"Outputs:   version=1.4.2", "Artifacts: report"}},
		{"the exit handler", &stub{phase: "Failed", exitHandler: true}, []string{"release.onExit", "notify failed"}},
		{"the start time", &stub{phase: "Pending", startAt: &at}, []string{"Starts:    " + at.Local().Format(time.DateTime)}},
		{"the parent and the child", &stub{phase: "Running", child: true}, []string{"Parent:    release-q4", "child run release-0a1b2c3d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(t, tt.stub, "get", "release")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("no %q in:\n%s", want, out)
				}
			}
		})
	}
}
