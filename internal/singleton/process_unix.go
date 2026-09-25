//go:build !windows

package singleton

import (
	"errors"
	"syscall"
)

// alive: signal 0 checks without sending; EPERM still means someone is there.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// detached gives the new instance a session of its own, so whatever ends the
// action's process group does not end the instance with it.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
