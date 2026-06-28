//go:build windows

package engine

import "syscall"

func processExists(pid int) (bool, error) {
	const processQueryLimitedInformation = 0x1000

	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// Error 87 means invalid parameter, which is what Windows returns for a dead/nonexistent PID here.
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.Errno(87) {
			return false, nil
		}
		// Access denied still proves that some process owns the PID.
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			return true, nil
		}
		return false, err
	}
	defer syscall.CloseHandle(handle)

	return true, nil
}
