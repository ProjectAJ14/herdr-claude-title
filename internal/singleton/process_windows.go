package singleton

import "syscall"

const (
	processQueryLimitedInformation = 0x1000
	detachedProcess                = 0x00000008
)

// alive waits zero time on the process: an exited process can still be
// opened while something holds a handle on it.
func alive(pid int) bool {
	handle, err := syscall.OpenProcess(
		processQueryLimitedInformation|syscall.SYNCHRONIZE,
		false,
		uint32(pid),
	) //nolint:gosec // a pid fits
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)

	event, err := syscall.WaitForSingleObject(handle, 0)

	return err == nil && event == uint32(syscall.WAIT_TIMEOUT)
}

// detached starts the instance without the action's console or group.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
