package executor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run runs a shell command with the executor and returns the exit code, the output and the message.
func run(t *testing.T, script string, env map[string]string) (int, string, Message) {
	t.Helper()
	dir := t.TempDir()
	term := filepath.Join(dir, "term")
	t.Setenv(EnvTermination, term)
	for k, v := range env {
		t.Setenv(k, strings.ReplaceAll(v, "$DIR", dir))
	}
	var out, errOut bytes.Buffer
	code := Run([]string{"sh", "-c", strings.ReplaceAll(script, "$DIR", dir)}, &out, &errOut)
	var msg Message
	data, err := os.ReadFile(term)
	if err != nil {
		t.Fatalf("no termination message: %v (stderr %q)", err, errOut.String())
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	return code, out.String(), msg
}

func TestRun(t *testing.T) {
	const outP = `[{"name":"p","path":"$DIR/p"}]`
	tests := []struct {
		name, script, outputs string
		code                  int
		check                 func(out string, m Message) bool
	}{
		{"result and parameter", "echo hello; echo -n v > $DIR/p", outP, 0,
			func(out string, m Message) bool {
				return out == "hello\n" && *m.Result == "hello" && m.Parameters["p"] == "v"
			}},
		{"failed command", "echo partial; exit 3", "", 3,
			func(_ string, m Message) bool { return *m.Result == "partial" }},
		{"missing output file", "true", outP, 1,
			func(_ string, m Message) bool { return strings.Contains(m.Error, `output parameter "p"`) }},
		{"large standard output", "head -c 5000 /dev/zero | tr '\\0' x", "", 0,
			func(_ string, m Message) bool { return m.Result == nil && m.ResultTooLarge }},
		{"large parameter", "head -c 5000 /dev/zero | tr '\\0' x > $DIR/p", outP, 1,
			func(_ string, m Message) bool { return strings.Contains(m.Error, "larger than 4 KiB") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{}
			if tt.outputs != "" {
				env[EnvOutputs] = tt.outputs
			}
			code, out, msg := run(t, tt.script, env)
			if code != tt.code || !tt.check(out, msg) {
				t.Errorf("code %d, out %q, msg %+v", code, out, msg)
			}
		})
	}
}

// TestRunScript runs a script from the work directory. The directory of the executor can be read-only (image volume).
func TestRunScript(t *testing.T) {
	work := t.TempDir()
	t.Setenv(EnvWorkDir, work)
	t.Setenv(EnvScript, "echo from script")
	t.Setenv(EnvTermination, filepath.Join(t.TempDir(), "term"))
	var out, errOut bytes.Buffer
	if code := Run([]string{"sh"}, &out, &errOut); code != 0 || out.String() != "from script\n" {
		t.Errorf("code %d, out %q, stderr %q", code, out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(work, "script")); err != nil {
		t.Errorf("the script is not in the work directory: %v", err)
	}
}

func TestInstall(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "exec")
	if err := Install(dst); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dst); err != nil || fi.Mode().Perm() != 0o555 {
		t.Errorf("installed file: %v %v", fi, err)
	}
}

func TestFitMessageKeepsSavedArtifacts(t *testing.T) {
	big := map[string]string{"p": strings.Repeat("x", MaxMessage)}
	got := fitMessage(Message{Parameters: big, Artifacts: []string{"data"}, Log: true})
	if got.Parameters != nil || len(got.Artifacts) != 1 || !got.Log || got.Error == "" {
		t.Errorf("message = %+v", got)
	}
}
