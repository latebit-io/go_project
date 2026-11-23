package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

const (
	// smokeEnv makes the test binary behave as the command itself.
	smokeEnv  = "TASKS_API_SMOKE"
	tasksPath = "/tasks"
)

// TestMain lets TestSmoke start the command as a real process without a
// separate build: with smokeEnv set, this binary runs main and nothing else.
func TestMain(m *testing.M) {
	if os.Getenv(smokeEnv) != "" {
		main()

		return
	}

	m.Run()
}

// started is the log line the command writes once it listens.
type started struct {
	Addr string `json:"addr"`
}

// step is one request to the running command and what must come back. In
// path, {id} stands for the task created earlier in the same scenario.
type step struct {
	method   string
	path     string
	body     string
	want     int
	contains string
}

// send performs the step and returns the response body.
func send(t *testing.T, base string, s step) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), s.method, base+s.path, strings.NewReader(s.body))
	if err != nil {
		t.Fatalf("build %s %s: %v", s.method, s.path, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", s.method, s.path, err)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", s.method, s.path, err)
	}

	if resp.StatusCode != s.want || !strings.Contains(string(body), s.contains) {
		t.Fatalf("%s %s = %d %s, want %d containing %q", s.method, s.path, resp.StatusCode, body, s.want, s.contains)
	}

	return string(body)
}

// createdID returns the id in a task response, or "" for any other body.
func createdID(body string) string {
	var created struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal([]byte(body), &created); err != nil {
		return ""
	}

	return created.ID
}

// command is the process under test and the address it logged.
type command struct {
	proc *exec.Cmd
	addr string
}

// launch starts the command on a free port, with its real environment
// handling and logging.
func launch(t *testing.T) command {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("find test binary: %v", err)
	}

	//nolint:gosec // the test starts its own binary
	cmd := exec.CommandContext(t.Context(), self)
	cmd.Env = append(os.Environ(), smokeEnv+"=1", "TASKS_ADDR=127.0.0.1:0")

	logs, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("pipe logs: %v", err)
	}

	err = cmd.Start()
	if err != nil {
		t.Fatalf("start command: %v", err)
	}

	// Every started process is waited for, also when the test fails early or
	// on Windows, where the test does not stop the command itself.
	t.Cleanup(func() {
		if cmd.ProcessState != nil {
			return
		}

		// The test context, already canceled here, may have ended the process;
		// Windows then refuses the kill with an error of its own. Either way
		// the process is gone, which is all the cleanup needs.
		if killErr := cmd.Process.Kill(); killErr != nil {
			t.Logf("stop command: %v", killErr)
		}

		t.Logf("command stopped: %v", cmd.Wait())
	})

	var first started

	// A command that dies before listening closes the pipe, so this cannot hang.
	line, err := bufio.NewReader(logs).ReadBytes('\n')
	if err != nil || json.Unmarshal(line, &first) != nil || first.Addr == "" {
		t.Fatalf("first log line = %q, %v, want the listen address", line, err)
	}

	return command{proc: cmd, addr: first.Addr}
}

// stop interrupts the command and requires a clean exit. Windows cannot
// deliver an interrupt to another process, so there the cleanup just ends it.
func stop(t *testing.T, running command) {
	t.Helper()

	if runtime.GOOS == "windows" {
		return
	}

	if err := running.proc.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt command: %v", err)
	}

	if err := running.proc.Wait(); err != nil {
		t.Fatalf("command exit after interrupt = %v, want a clean shutdown", err)
	}
}

// TestSmoke proves the built command works: it starts, serves real HTTP,
// keeps state between requests and exits cleanly when interrupted.
func TestSmoke(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		steps []step
	}{
		{
			name: "a task survives between requests",
			steps: []step{
				{http.MethodPost, tasksPath, `{"title":"smoke"}`, http.StatusCreated, `"done":false`},
				{http.MethodPost, tasksPath + "/{id}/complete", "", http.StatusOK, `"done":true`},
				{http.MethodGet, tasksPath, "", http.StatusOK, `"done":true`},
			},
		},
		{
			name: "bad input is refused and the command keeps serving",
			steps: []step{
				{http.MethodPost, tasksPath, `{"title":" "}`, http.StatusUnprocessableEntity, `"error"`},
				{http.MethodGet, tasksPath, "", http.StatusOK, "[]"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			running := launch(t)
			id := ""

			for _, s := range tt.steps {
				s.path = strings.ReplaceAll(s.path, "{id}", id)

				if created := createdID(send(t, "http://"+running.addr, s)); created != "" {
					id = created
				}
			}

			stop(t, running)
		})
	}
}
