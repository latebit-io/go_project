package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// smokeEnv makes the test binary behave as the command itself.
const smokeEnv = "TASKS_API_SMOKE"

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

type task struct {
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

type request struct {
	method string
	url    string
	body   string
}

// call sends one request to the running command and decodes the JSON answer.
func call(t *testing.T, r request, into any) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), r.method, r.url, strings.NewReader(r.body))
	if err != nil {
		t.Fatalf("build %s %s: %v", r.method, r.url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", r.method, r.url, err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response: %v", err)
		}
	}()

	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("decode %s %s: %v", r.method, r.url, err)
	}

	return resp.StatusCode
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

	var first started

	// A command that dies before listening closes the pipe, so this cannot hang.
	line, err := bufio.NewReader(logs).ReadBytes('\n')
	if err != nil || json.Unmarshal(line, &first) != nil || first.Addr == "" {
		t.Fatalf("first log line = %q, %v, want the listen address", line, err)
	}

	return command{proc: cmd, addr: first.Addr}
}

// TestSmoke proves the built command works: it starts, serves real HTTP,
// keeps a task between requests and exits cleanly when interrupted.
func TestSmoke(t *testing.T) {
	t.Parallel()

	running := launch(t)
	tasks := "http://" + running.addr + "/tasks"

	var created, completed task
	if code := call(t, request{http.MethodPost, tasks, `{"title":"smoke"}`}, &created); code != http.StatusCreated {
		t.Fatalf("POST /tasks status = %d, want 201", code)
	}

	complete := request{method: http.MethodPost, url: tasks + "/" + created.ID + "/complete"}
	if code := call(t, complete, &completed); code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200", code)
	}

	var listed []task

	code := call(t, request{method: http.MethodGet, url: tasks}, &listed)
	if code != http.StatusOK || len(listed) != 1 || !listed[0].Done {
		t.Fatalf("GET /tasks = %d %+v, want the one completed task", code, listed)
	}

	// Windows cannot deliver an interrupt to another process, so only the
	// other platforms prove the graceful path; there the test just ends it.
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
