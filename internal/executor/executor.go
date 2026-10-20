// Package executor runs the main command of a workflow Pod and reports its outputs
// through the container termination message. The Pod needs no Kubernetes API access.
package executor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The environment variables that the controller sets on the main container.
const (
	EnvOutputs     = "CHRONICLE_OUTPUTS"
	EnvScript      = "CHRONICLE_SCRIPT"
	EnvTermination = "CHRONICLE_TERMINATION_PATH"
)

// MaxMessage is the Kubernetes limit for a termination message.
const MaxMessage = 4096

// maxResult is the largest standard output that becomes the result.
const maxResult = 3072

// Output is a declared output parameter: its name and the file that holds its value.
type Output struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Message is the termination message that the controller reads.
type Message struct {
	Result *string `json:"result,omitempty"`
	// ResultTooLarge is true when the standard output was too large to be the result.
	ResultTooLarge bool              `json:"resultTooLarge,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	// Artifacts are the names of the saved output artifacts. The keys come from the prefix of the Pod.
	Artifacts []string `json:"artifacts,omitempty"`
	// Log is true when the log is archived.
	Log   bool   `json:"log,omitempty"`
	Error string `json:"error,omitempty"`
}

// Install copies the running binary to dst, so that the main container can run it.
// It streams the copy, because the binary is large and the init container has a small memory limit.
func Install(dst string) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the executable: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o555)
	if err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

// killGrace is the time between the first stop signal and the kill of the command. It leaves time to save the log
// within the 30 s grace period of the Pod.
var killGrace = 20 * time.Second

// Run runs the command, writes the termination message and returns the exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec: no command")
		return 2
	}
	args, err := addScript(args)
	if err != nil {
		return fail(stderr, err)
	}
	f, err := openFiles()
	if err == nil {
		err = f.download()
	}
	if err != nil {
		return fail(stderr, err)
	}
	var captured limitedBuffer
	log := f.logWriter()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = childEnv()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, io.MultiWriter(stdout, &captured, log), io.MultiWriter(stderr, log)
	code := runChild(cmd, stderr)
	msg := collect(&captured)
	f.save(&msg, code == 0, stderr)
	msg = fitMessage(msg)
	if msg.Error != "" {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", msg.Error)
		if code == 0 {
			code = 1
		}
	}
	if err := writeMessage(msg); err != nil {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", err)
	}
	return code
}

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintln(stderr, "chronicle-exec:", err)
	_ = writeMessage(Message{Error: err.Error()})
	return 1
}

// addScript writes the script source next to the executor and adds its path as the last argument.
func addScript(args []string) ([]string, error) {
	src, ok := readScript()
	if !ok {
		return args, nil
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the executable: %w", err)
	}
	path := filepath.Join(filepath.Dir(self), "script")
	if err := os.WriteFile(path, []byte(src), 0o555); err != nil {
		return nil, fmt.Errorf("write the script: %w", err)
	}
	return append(args, path), nil
}

// runChild runs the command, forwards SIGTERM and SIGINT, and returns the exit code.
func runChild(cmd *exec.Cmd, stderr io.Writer) int {
	// The command and its children get their own process group, so that a signal reaches all of them.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A child that keeps the output open must not keep the executor from saving the log.
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintln(stderr, "chronicle-exec:", err)
		return 127
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)
	go func() {
		var kill sync.Once
		for s := range sigs {
			_ = syscall.Kill(-cmd.Process.Pid, s.(syscall.Signal))
			// A shell that waits for a child ignores SIGTERM until the child ends. Kubernetes kills the Pod after its
			// grace period (30 s), and then the log is lost. So the executor kills the command first.
			kill.Do(func() {
				time.AfterFunc(killGrace, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			})
		}
	}()
	err := cmd.Wait()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return exitErr.ExitCode()
	}
	_, _ = fmt.Fprintln(stderr, "chronicle-exec:", err)
	return 1
}

// collect reads the output files and the result.
func collect(captured *limitedBuffer) Message {
	var msg Message
	if captured.overflow {
		msg.ResultTooLarge = true
	} else {
		r := strings.TrimSuffix(captured.String(), "\n")
		msg.Result = &r
	}
	var outputs []Output
	if s := os.Getenv(EnvOutputs); s != "" {
		if err := json.Unmarshal([]byte(s), &outputs); err != nil {
			msg.Error = fmt.Sprintf("bad %s: %v", EnvOutputs, err)
			return msg
		}
	}
	for _, o := range outputs {
		data, err := os.ReadFile(o.Path)
		if err != nil {
			msg.Error = fmt.Sprintf("output parameter %q: %v", o.Name, err)
			return msg
		}
		if msg.Parameters == nil {
			msg.Parameters = map[string]string{}
		}
		msg.Parameters[o.Name] = strings.TrimSuffix(string(data), "\n")
	}
	return msg
}

// fitMessage makes the message fit in MaxMessage. It drops the result first, then the parameters.
// It keeps the saved artifacts and the log, because their objects are already in the repository.
func fitMessage(msg Message) Message {
	if data, _ := json.Marshal(msg); len(data) > MaxMessage && msg.Result != nil {
		msg.Result, msg.ResultTooLarge = nil, true
	}
	if data, _ := json.Marshal(msg); len(data) <= MaxMessage {
		return msg
	}
	small := Message{Error: "the output parameters are larger than 4 KiB: use an artifact", Artifacts: msg.Artifacts, Log: msg.Log}
	if data, _ := json.Marshal(small); len(data) > MaxMessage {
		return Message{Error: "the names of the output artifacts are larger than 4 KiB: use fewer artifacts", Log: msg.Log}
	}
	return small
}

// writeMessage writes the message as JSON to the termination message file.
func writeMessage(msg Message) error {
	data, _ := json.Marshal(msg)
	path := os.Getenv(EnvTermination)
	if path == "" {
		path = "/dev/termination-log"
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write the termination message: %w", err)
	}
	return nil
}

// limitedBuffer keeps up to maxResult bytes and records an overflow.
type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.overflow || b.Len()+len(p) > maxResult {
		b.overflow = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
