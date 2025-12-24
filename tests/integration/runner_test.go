package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type ExecSpec struct {
	Cmd     string
	Args    []string
	Dir     string
	Env     map[string]string
	Prefix  string
	Timeout time.Duration
}

type ExecResult struct {
	ExitCode int
	Output   string
}

type Runner struct {
	t   testing.TB
	out io.Writer
}

func NewRunner(t testing.TB) *Runner {
	t.Helper()
	return &Runner{t: t, out: os.Stdout}
}

func (r *Runner) Exec(ctx context.Context, spec ExecSpec) (ExecResult, error) {
	r.t.Helper()

	if strings.TrimSpace(spec.Cmd) == "" {
		return ExecResult{}, errors.New("command is required")
	}

	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, spec.Cmd, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = mergeEnv(os.Environ(), spec.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ExecResult{}, err
	}

	prefix := strings.TrimSpace(spec.Prefix)
	if prefix != "" {
		prefix += " "
	}

	_, _ = fmt.Fprintf(r.out, "%s$ %s\n", prefix, formatCommand(spec.Cmd, spec.Args))

	if err := cmd.Start(); err != nil {
		return ExecResult{}, err
	}

	var combined bytes.Buffer
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(2)

	streamLine := func(marker string, line string) {
		_, _ = fmt.Fprintf(r.out, "%s%s %s\n", prefix, marker, line)
		mu.Lock()
		combined.WriteString(line)
		combined.WriteByte('\n')
		mu.Unlock()
	}

	go func() {
		defer wg.Done()
		scanLines(stdout, func(line string) { streamLine("|", line) })
	}()
	go func() {
		defer wg.Done()
		scanLines(stderr, func(line string) { streamLine("!", line) })
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	out := strings.TrimRight(combined.String(), "\n")
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else if errors.Is(waitErr, context.DeadlineExceeded) {
			exitCode = 124
		} else {
			exitCode = 1
		}
	}

	return ExecResult{ExitCode: exitCode, Output: out}, waitErr
}

func scanLines(r io.Reader, handle func(string)) {
	const maxLine = 1024 * 1024

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxLine)
	for scanner.Scan() {
		handle(scanner.Text())
	}
	// Don't report EOF or "file already closed" errors - these are expected when the process finishes
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "file already closed") {
		handle(fmt.Sprintf("scanner error: %v", err))
	}
}

func formatCommand(cmd string, args []string) string {
	parts := append([]string{cmd}, args...)
	return strings.Join(parts, " ")
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}

	merged := make([]string, 0, len(base)+len(extra))
	used := make(map[string]struct{}, len(extra))

	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			merged = append(merged, kv)
			continue
		}
		if v, ok := extra[key]; ok {
			merged = append(merged, key+"="+v)
			used[key] = struct{}{}
			continue
		}
		merged = append(merged, kv)
	}

	keys := make([]string, 0, len(extra))
	for k := range extra {
		if _, ok := used[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		merged = append(merged, k+"="+extra[k])
	}

	return merged
}

func containsLine(out string, want string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if line == want {
			return true
		}
	}
	return false
}
