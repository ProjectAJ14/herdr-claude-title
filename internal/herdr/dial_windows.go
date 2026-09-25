package herdr

import (
	"context"
	"io"
	"os"
)

// On Windows HERDR_SOCKET_PATH names a small file, and Herdr listens on the
// named pipe carrying that whole path as its name.
const pipePrefix = `\\.\pipe\`

// dial opens the pipe. A pipe takes no deadline, but closing it unblocks a
// pending read, which is all cancellation needs.
func dial(_ context.Context, path string) (io.ReadWriteCloser, error) {
	//nolint:gosec // the path is HERDR_SOCKET_PATH, never terminal-derived
	return os.OpenFile(pipePrefix+path, os.O_RDWR, 0)
}

// serverIdentity is the pid:start-time marker Herdr writes into the file.
func serverIdentity(path string) string {
	marker, err := os.ReadFile(path) //nolint:gosec // HERDR_SOCKET_PATH
	if err != nil {
		return ""
	}

	return string(marker)
}
