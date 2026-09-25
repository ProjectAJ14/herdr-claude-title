// Package singleton keeps one plugin instance per Herdr session. Herdr runs
// startup hooks at every server start and live handoff and keeps no handle
// on what they start, so instances would pile up and fight over labels.
// Instead each instance claims the session in a file; the newest claim wins
// and older instances leave. `restart` is a new claim waiting for the old one.
package singleton

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const pollEvery = 50 * time.Millisecond

var errNoStateDir = errors.New("no state directory to claim the session in")

type record struct {
	PID int `json:"pid"`
}

// ClaimPath names the claim file for a session. The socket path names the
// session; it is hashed because a path is not a file name.
func ClaimPath(stateDir, socketPath string) string {
	if stateDir == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(socketPath))

	return filepath.Join(stateDir, "instances", hex.EncodeToString(sum[:8])+".json")
}

// readyPath is a file of its own so marking ready never overwrites a claim a
// newer instance took meanwhile.
func readyPath(claimPath string) string {
	return strings.TrimSuffix(claimPath, filepath.Ext(claimPath)) + ".ready.json"
}

// Claim is this process's hold on a session.
type Claim struct {
	path  string
	pid   int
	ready bool
}

// Take claims the session, displacing its holder, and waits up to leaveWithin
// for that holder to exit. It kills nothing; it returns the pid that stayed
// (0 if none) and an error worth a warning, never a stop.
func Take(ctx context.Context, path string, leaveWithin time.Duration) (*Claim, int, error) {
	c := &Claim{path: path, pid: os.Getpid()}
	if path == "" {
		return c, 0, errNoStateDir
	}

	displaced := holder(path)
	if displaced == c.pid {
		displaced = 0
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return c, displaced, err
	}

	if err := write(path, c.pid); err != nil {
		return c, displaced, err
	}

	if displaced == 0 || waitUntil(ctx, leaveWithin, func() bool { return !alive(displaced) }) {
		return c, 0, nil
	}

	return c, displaced, nil
}

// Superseded reports that a newer instance holds the claim. An unreadable
// claim decides nothing (it may be half written); a missing one is nobody's.
func (c *Claim) Superseded() bool {
	pid, ok := read(c.path)
	return ok && pid != c.pid
}

// MarkReady records that this instance has read the session once.
func (c *Claim) MarkReady() {
	if c.path != "" && !c.ready {
		c.ready = write(readyPath(c.path), c.pid) == nil
	}
}

// Restart starts exe detached and returns the pid naming the session once
// it is ready and the instance it displaced has gone.
func Restart(ctx context.Context, path, exe string, timeout time.Duration) (int, error) {
	if path == "" {
		return 0, errNoStateDir // no claim: the old instance would never leave
	}

	previous := holder(path)

	proc, err := startDetached(exe)
	if err != nil {
		return 0, fmt.Errorf("start %s: %w", exe, err)
	}

	exited := make(chan *os.ProcessState, 1)

	go func() {
		state, _ := proc.Wait() // reaping it is what lets alive see it gone
		exited <- state
	}()

	var state *os.ProcessState

	done := waitUntil(ctx, timeout, func() bool {
		select {
		case state = <-exited:
			return true
		default:
			return stillWaiting(path, proc.Pid, previous) == nil
		}
	})

	switch {
	case state != nil:
		// A second restart a moment later took over: that is a success too.
		if newer := holder(path); newer != 0 && newer != previous {
			return newer, nil
		}

		return 0, fmt.Errorf("the new instance %d exited before it read the session: %v", proc.Pid, state)
	case done:
		return proc.Pid, nil
	case ctx.Err() != nil:
		return 0, fmt.Errorf("%w; instance %d is left to take over on its own", ctx.Err(), proc.Pid)
	}

	if left := stillWaiting(path, proc.Pid, previous); left != nil {
		return 0, fmt.Errorf("gave up after %s: %w", timeout, left)
	}

	return proc.Pid, nil
}

func stillWaiting(path string, pid, previous int) error {
	switch readyPID, _ := read(readyPath(path)); {
	case holder(path) != pid:
		return fmt.Errorf("the new instance %d has not claimed the session", pid)
	case readyPID != pid:
		return fmt.Errorf("the new instance %d has not read the session", pid)
	case previous != 0 && alive(previous):
		return fmt.Errorf("instance %d is polling but %d is still running; `herdr server stop` ends it", pid, previous)
	}

	return nil
}

// startDetached runs exe with stdio on the null device: Herdr reads an
// action's output to EOF, so a child holding its pipes would keep the action
// (and one of 32 plugin command slots) busy for its whole life.
func startDetached(exe string) (*os.Process, error) {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer null.Close()

	return os.StartProcess(exe, []string{exe}, &os.ProcAttr{
		Env:   os.Environ(),
		Files: []*os.File{null, null, null},
		Sys:   detached(),
	})
}

func holder(path string) int {
	if pid, ok := read(path); ok && alive(pid) {
		return pid
	}

	return 0
}

func waitUntil(ctx context.Context, timeout time.Duration, done func() bool) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	tick := time.NewTicker(pollEvery)
	defer tick.Stop()

	for !done() {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}

	return true
}

func read(path string) (int, bool) {
	raw, err := os.ReadFile(path) //nolint:gosec // the plugin's own state file
	if err != nil {
		return 0, false
	}

	var rec record
	if json.Unmarshal(raw, &rec) != nil || rec.PID <= 0 {
		return 0, false
	}

	return rec.PID, true
}

// write is one call, not temp+rename: a one-line file read half-written is
// treated as "nothing said" and read again next poll.
func write(path string, pid int) error {
	raw, _ := json.Marshal(record{PID: pid})
	return os.WriteFile(path, raw, 0o600)
}
