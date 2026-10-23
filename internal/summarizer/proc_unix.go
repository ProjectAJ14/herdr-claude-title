//go:build !windows

package summarizer

import "syscall"

func hidden() *syscall.SysProcAttr { return nil }
