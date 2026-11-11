package bunkoscaffold

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Command struct {
	Git     bool
	Path    string
	Args    []string
	Dir     string
	Input   []byte
	Timeout time.Duration
}
type Runner interface {
	LookPath(string) (string, error)
	Run(Command) ([]byte, error)
	Interactive(Command, io.Reader, io.Writer, io.Writer) error
}
type OSRunner struct{}

func (OSRunner) LookPath(name string) (string, error) {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return exec.LookPath(name)
}

type boundedOutput struct {
	bytes.Buffer
	remaining int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.remaining {
		p = p[:b.remaining]
	}
	b.Buffer.Write(p)
	b.remaining -= len(p)
	return n, nil
}
func process(c Command) (context.Context, context.CancelFunc, *exec.Cmd) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Dir = c.Dir
	cmd.WaitDelay = 2 * time.Second
	if c.Git {
		// Scope Git to the selected package, not inherited GIT_DIR/config/trace overrides.
		// Other variables are inherited unchanged; credentials are never inspected.
		environment := cmd.Environ()
		clean := make([]string, 0, len(environment))
		for _, entry := range environment {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
				clean = append(clean, entry)
			}
		}
		cmd.Env = clean
	}
	return ctx, cancel, cmd
}
func (OSRunner) Run(c Command) ([]byte, error) {
	if c.Timeout <= 0 {
		return nil, invalid("process", "")
	}
	ctx, cancel, cmd := process(c)
	defer cancel()
	var out = boundedOutput{remaining: 64 << 10}
	var stderr = boundedOutput{remaining: 64 << 10}
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Stdin = bytes.NewReader(c.Input)
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, invalid("timeout", "")
	}
	if err != nil {
		return nil, invalid("process", "")
	}
	if out.remaining == 0 {
		return nil, invalid("process", "")
	}
	return out.Bytes(), nil
}
func (OSRunner) Interactive(c Command, in io.Reader, out, stderr io.Writer) error {
	if c.Timeout <= 0 {
		return invalid("process", "")
	}
	ctx, cancel, cmd := process(c)
	defer cancel()
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return invalid("timeout", "")
	}
	if err != nil {
		return invalid("process", "")
	}
	return nil
}

type Tool struct{ Path, Version string }
type Environment struct{ Git, Gh Tool }

func detect(r Runner) Environment {
	var e Environment
	for _, name := range []string{"git", "gh"} {
		p, err := r.LookPath(name)
		if err != nil {
			continue
		}
		b, err := r.Run(Command{Path: p, Git: name == "git", Args: []string{"--version"}, Timeout: 5 * time.Second})
		if err != nil {
			continue
		}
		v := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0]
		t := Tool{p, v}
		if name == "git" {
			e.Git = t
		} else {
			e.Gh = t
		}
	}
	return e
}
func isDeclined(err error) bool { return errors.Is(err, errDeclined) }

var errDeclined = errors.New("operation declined")
