package singleton

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNewestClaimWinsAndReadyIsSeparate(t *testing.T) {
	path := ClaimPath(t.TempDir(), "/tmp/herdr.sock")

	c, stayed, err := Take(context.Background(), path, time.Second)
	if err != nil || stayed != 0 || c.Superseded() {
		t.Fatalf("take: stayed=%d err=%v superseded=%v", stayed, err, c.Superseded())
	}

	c.MarkReady()

	if pid, _ := read(readyPath(path)); pid != os.Getpid() {
		t.Fatalf("ready marker names %d", pid)
	}

	write(path, os.Getppid()) // a newer instance (a live pid) claims the session

	if !c.Superseded() {
		t.Fatal("a newer claim must supersede this instance")
	}

	os.Remove(path)

	if c.Superseded() {
		t.Fatal("a missing claim is nobody's, and ends no run")
	}
}

func TestNoStateDirIsAWarningNotAStop(t *testing.T) {
	if _, _, err := Take(context.Background(), ClaimPath("", "s"), time.Second); err == nil {
		t.Fatal("want an error to warn about")
	}
}
