//go:build !windows

package refinery

import (
	"errors"
	"syscall"
)

// claimantProcessAlive reports whether process pid exists. It fails closed:
// a process that exists but belongs to another user (EPERM) counts as alive.
func claimantProcessAlive(pid int) bool {
	if pid <= 0 {
		return true
	}
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}
