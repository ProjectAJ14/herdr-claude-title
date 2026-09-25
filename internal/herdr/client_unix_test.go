//go:build !windows

package herdr

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// serve imitates Herdr: one request per connection, answered, then closed.
func serve(t *testing.T, reply func(line []byte) string) *SocketClient {
	t.Helper()

	//nolint:usetesting // t.TempDir() overruns the 104 bytes of a socket's sun_path
	dir, _ := os.MkdirTemp("", "hc")
	t.Cleanup(func() { os.RemoveAll(dir) })

	path := filepath.Join(dir, "h.sock")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			if r := reply(line); r != "" {
				conn.Write([]byte(r + "\n"))
			}

			conn.Close()
		}
	}()

	return &SocketClient{path: path}
}

func TestSnapshotDecodesAndNullsAreEmpty(t *testing.T) {
	c := serve(t, func([]byte) string {
		return `{"id":"x","result":{"snapshot":{"tabs":[{"tab_id":"t1","label":null}],` +
			`"panes":[{"pane_id":"p1","cwd":null,"agent_session":null,"revision":3}]}}}`
	})

	snap, err := ReadSnapshot(context.Background(), c)
	if err != nil || len(snap.Panes) != 1 || snap.Panes[0].CWD != "" || snap.Tabs[0].Label != "" {
		t.Fatalf("snap = %+v, err = %v", snap, err)
	}

	if c.ServerIdentity() == "" {
		t.Error("a bound socket has an identity")
	}
}

func TestErrorsCarryCodesAndSilenceIsUnanswered(t *testing.T) {
	c := serve(t, func([]byte) string { return `{"id":"x","error":{"code":"tab_not_found","message":"gone"}}` })
	if err := RenameTab(context.Background(), c, "t1", "x"); ErrorCode(err) != CodeTabNotFound {
		t.Errorf("err = %v", err)
	}

	silent := serve(t, func([]byte) string { return "" })
	if err := RenameTab(context.Background(), silent, "t1", "x"); !errors.Is(err, ErrUnanswered) {
		t.Errorf("a read-but-unanswered call must be ErrUnanswered, got %v", err)
	}

	never := &SocketClient{path: filepath.Join(t.TempDir(), "none.sock")}
	if err := RenameTab(context.Background(), never, "t1", "x"); err == nil || errors.Is(err, ErrUnanswered) {
		t.Errorf("a call that never reached Herdr must not be ErrUnanswered, got %v", err)
	}
}

func TestEveryMethodSendsWhatHerdrExpects(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)

	c := serve(t, func(line []byte) string {
		mu.Lock()
		seen = append(seen, string(line))
		mu.Unlock()

		switch {
		case strings.Contains(string(line), "pane.process_info"):
			return `{"id":"x","result":{"process_info":{"foreground_processes":[{"name":"nvim","argv":["nvim"],"cwd":"/w"}]}}}`
		case strings.Contains(string(line), "notification.show"):
			return `{"id":"x","result":{"shown":false,"reason":"no_foreground_client"}}`
		}

		return `{"id":"x","result":{}}`
	})
	ctx := context.Background()

	if _, err := ReadSnapshot(ctx, c); err != nil {
		t.Fatal(err)
	}

	procs, err := ForegroundProcesses(ctx, c, "p1")
	if err != nil || len(procs) != 1 || procs[0].Name != "nvim" || procs[0].CWD != "/w" {
		t.Fatalf("processes = %+v, %v", procs, err)
	}

	if err := RenamePane(ctx, c, "p1", "api"); err != nil {
		t.Fatal(err)
	}

	if err := RenameWorkspace(ctx, c, "w1", "api"); err != nil {
		t.Fatal(err)
	}

	res, err := Notify(ctx, c, "Claude Title restarted", "pid 1")
	if err != nil || res.Shown || res.Reason != "no_foreground_client" {
		t.Fatalf("notify = %+v, %v", res, err)
	}

	mu.Lock()
	defer mu.Unlock()

	for _, want := range []string{`"pane_id":"p1"`, `"workspace_id":"w1"`, `"title":"Claude Title restarted"`, `"params":{}`} {
		if !strings.Contains(strings.Join(seen, "\n"), want) {
			t.Errorf("no request carried %s", want)
		}
	}
}

func TestACancelledCallIsUnanswered(t *testing.T) {
	c := serve(t, func([]byte) string { time.Sleep(2 * time.Second); return "" })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := RenameTab(ctx, c, "t1", "x")
	if !errors.Is(err, ErrUnanswered) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want unanswered by the deadline", err)
	}
}
