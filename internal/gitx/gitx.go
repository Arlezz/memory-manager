// Package gitx wraps the git binary.
//
// We shell out to git instead of using a Go git library on purpose: git inherits
// the user's credential helpers, SSH agent and proxy configuration. Real remotes
// in use here span GitHub over SSH, GitHub over HTTPS and a self-hosted GitLab,
// so reimplementing auth would be the largest source of bugs in this tool.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ErrNotFound reports that no git executable is available on PATH.
var ErrNotFound = errors.New("git executable not found on PATH")

// defaultTimeout bounds any git call. Network operations must never hang a
// session: the caller degrades to local memory when we time out.
const defaultTimeout = 30 * time.Second

// Run executes git with args inside dir and returns trimmed stdout.
// The error, when non-nil, carries git's stderr so callers can surface it.
func Run(dir string, args ...string) (string, error) {
	return RunWithTimeout(dir, defaultTimeout, args...)
}

// RunWithTimeout is Run with an explicit deadline.
func RunWithTimeout(dir string, timeout time.Duration, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", ErrNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Refuse any interactive prompt. Without this a missing credential blocks
	// the SessionStart hook behind a password prompt nobody can see.
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=echo",
		"GCM_INTERACTIVE=never",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git %s timed out after %s", args[0], timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

// RemoteURL returns the fetch URL of the named remote, as configured.
//
// The result may contain credentials. Never persist or log it directly; run it
// through identity.Normalize first.
func RemoteURL(dir, remote string) (string, error) {
	return Run(dir, "remote", "get-url", remote)
}

// TopLevel returns the absolute root of the work tree containing dir.
func TopLevel(dir string) (string, error) {
	return Run(dir, "rev-parse", "--show-toplevel")
}

// IsClean reports whether the work tree has no uncommitted changes.
func IsClean(dir string) (bool, error) {
	out, err := Run(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}
