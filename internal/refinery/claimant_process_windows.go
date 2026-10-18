//go:build windows

package refinery

import (
	"errors"
	"math"

	"golang.org/x/sys/windows"
)

// claimantProcessAlive reports whether process pid exists. It fails closed:
// only "no such process" (ERROR_INVALID_PARAMETER) counts as dead; access
// denied or any other error counts as alive.
func claimantProcessAlive(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return true
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	_ = windows.CloseHandle(handle)
	return true
}
