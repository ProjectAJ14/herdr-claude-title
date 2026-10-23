package summarizer

import "syscall"

const createNoWindow = 0x08000000

// hidden keeps `claude -p` from opening a console window: the plugin runs
// detached with no console, so Windows would give the child a visible one.
func hidden() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
