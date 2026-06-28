package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// lockHandle tracks the process-local ownership of the database lock file.
type lockHandle struct {
	path string
	file *os.File
}

func acquireLock(dataDir string) (*lockHandle, error) {
	lockPath := filepath.Join(dataDir, "minidb.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrDatabaseLocked, lockPath)
		}

		return nil, fmt.Errorf("create database lock file: %w", err)
	}

	// Writing the PID makes the lock file more inspectable when users troubleshoot the DB folder.
	if _, err := lockFile.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		lockFile.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("write lock file pid: %w", err)
	}

	if err := lockFile.Sync(); err != nil {
		lockFile.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("sync lock file: %w", err)
	}

	return &lockHandle{
		path: lockPath,
		file: lockFile,
	}, nil
}

func (l *lockHandle) release() error {
	if l == nil {
		return nil
	}

	if err := l.file.Close(); err != nil {
		return fmt.Errorf("close lock file: %w", err)
	}

	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove lock file: %w", err)
	}

	return nil
}
