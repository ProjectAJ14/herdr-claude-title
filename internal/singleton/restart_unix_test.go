//go:build !windows

package singleton

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeInstance is a stand-in binary: it does what a real instance does on
// start (claim, then mark ready) as far as the script body says.
func fakeInstance(t *testing.T, body string) string {
	t.Helper()

	exe := filepath.Join(t.TempDir(), "instance")
	script := "#!/bin/sh\nclaim=\"$FAKE_CLAIM\"\nready=\"${claim%.json}.ready.json\"\n" + body + "\n"

	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	return exe
}

const claims = `echo "{\"pid\":$$}" > "$claim"`
const readies = `echo "{\"pid\":$$}" > "$ready"`

func TestRestartWaitsForTheNewInstanceToBeReady(t *testing.T) {
	path := ClaimPath(t.TempDir(), "/tmp/h.sock")
	os.MkdirAll(filepath.Dir(path), 0o700)
	t.Setenv("FAKE_CLAIM", path)

	pid, err := Restart(
		context.Background(),
		path,
		fakeInstance(t, claims+"\nsleep 0.2\n"+readies+"\nexec sleep 30"),
		5*time.Second,
	)
	if err != nil || pid <= 0 {
		t.Fatalf("pid = %d, err = %v", pid, err)
	}

	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })

	if holder(path) != pid {
		t.Error("the new instance holds the claim")
	}
}

func TestRestartReportsWhatItIsStillWaitingFor(t *testing.T) {
	cases := []struct {
		name, body, want string
		previousAlive    bool
	}{
		{"exits at once", "exit 1", "exited before it read the session", false},
		{"never claims", "exec sleep 5", "has not claimed the session", false},
		{"never ready", claims + "\nexec sleep 5", "has not read the session", false},
		{"old one stays", claims + "\n" + readies + "\nexec sleep 5", "herdr server stop", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := ClaimPath(t.TempDir(), "/tmp/h.sock")
			os.MkdirAll(filepath.Dir(path), 0o700)
			t.Setenv("FAKE_CLAIM", path)

			if c.previousAlive {
				write(path, os.Getpid()) // this test process plays the instance that will not leave
			}

			_, err := Restart(context.Background(), path, fakeInstance(t, c.body), 2*time.Second)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to mention %q", err, c.want)
			}

			if pid, ok := read(path); ok && pid != os.Getpid() {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		})
	}
}

func TestRestartRefusesWithoutAClaimAndHonoursCancellation(t *testing.T) {
	if _, err := Restart(context.Background(), "", "/bin/true", time.Second); err == nil {
		t.Error("no state dir: nothing could be waited for, so nothing may start")
	}

	path := ClaimPath(t.TempDir(), "/tmp/h.sock")
	os.MkdirAll(filepath.Dir(path), 0o700)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	exe := fakeInstance(t, "exec sleep 5")
	if _, err := Restart(
		ctx,
		path,
		exe,
		time.Minute,
	); err == nil ||
		!strings.Contains(err.Error(), "left to take over") {
		t.Errorf("err = %v", err)
	}

	if _, err := Restart(context.Background(), path, filepath.Join(t.TempDir(), "missing"), time.Second); err == nil {
		t.Error("a binary that cannot start must fail")
	}
}

func TestTakeWaitsForTheDisplacedInstance(t *testing.T) {
	path := ClaimPath(t.TempDir(), "/tmp/h.sock")
	os.MkdirAll(filepath.Dir(path), 0o700)
	write(path, os.Getppid()) // a live pid that will not leave

	if _, stayed, err := Take(context.Background(), path, 150*time.Millisecond); err != nil || stayed != os.Getppid() {
		t.Fatalf("stayed = %d, err = %v", stayed, err)
	}
}
