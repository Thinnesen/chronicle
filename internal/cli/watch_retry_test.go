package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	chroniclev1 "github.com/Thinnesen/chronicle/gen/chronicle/v1"
	"github.com/Thinnesen/chronicle/gen/chronicle/v1/chroniclev1connect"
)

// pendingRetry is a fake server with a retry that the controller has not applied yet. The first reads show the old
// phase Failed of the run.
type pendingRetry struct {
	*stub
	reads int
}

func (s *pendingRetry) GetWorkflow(ctx context.Context, r *connect.Request[chroniclev1.GetWorkflowRequest]) (*connect.Response[chroniclev1.GetWorkflowResponse], error) {
	resp, err := s.stub.GetWorkflow(ctx, r)
	if err != nil || s.reads == 0 {
		return resp, err
	}
	s.reads--
	wf, err := structpb.NewStruct(map[string]any{"metadata": map[string]any{"name": r.Msg.Name},
		"spec": map[string]any{"restart": map[string]any{"generation": 1}}, "status": map[string]any{"restarts": 0}})
	if err != nil {
		return nil, err
	}
	resp.Msg.Workflow, resp.Msg.Summary.Phase = wf, "Failed"
	return resp, nil
}

// TestWatchPendingRetry checks that a watch right after "chronicle retry" waits for the retry and does not end with the
// old phase.
func TestWatchPendingRetry(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle(chroniclev1connect.NewWorkflowServiceHandler(&pendingRetry{stub: &stub{phase: "Succeeded", restarted: "True"}, reads: 2}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := (&Config{Server: srv.URL, Namespace: "team-a", Token: "tok"}).save(cfgPath); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := NewCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--config", cfgPath, "watch", "x"})
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "x Succeeded") {
		t.Errorf("watch of a pending retry: %v\n%s", err, out.String())
	}
}
