//go:build !windows

package herdr

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

func dial(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}

// serverIdentity is the socket file's device and inode: a server binding the
// path creates the file anew, so a successor's identity differs.
func serverIdentity(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}

	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
