package engine

// LockProcessExistsFunc is the function shape used by tests to simulate stale or live lock owners.
type LockProcessExistsFunc func(pid int) (bool, error)

// SetProcessExistsForLockForTests temporarily overrides the process existence check used by lock recovery.
func SetProcessExistsForLockForTests(fn LockProcessExistsFunc) LockProcessExistsFunc {
	previous := processExistsForLock
	processExistsForLock = fn
	return previous
}

// RestoreProcessExistsForLockForTests restores the prior process existence check after a test override.
func RestoreProcessExistsForLockForTests(previous LockProcessExistsFunc) {
	processExistsForLock = previous
}
