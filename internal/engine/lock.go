package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// lockHandle tracks the process-local ownership of the database lock file.
type lockHandle struct {
	path string
	file *os.File
}

var processExistsForLock = processExists

func acquireLock(dataDir string) (*lockHandle, error) {
	lockPath := filepath.Join(dataDir, "minidb.lock")
	for {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
		if err == nil {
			if err := writeLockPID(lockFile, lockPath); err != nil {
				return nil, err
			}

			return &lockHandle{
				path: lockPath,
				file: lockFile,
			}, nil
		}

		if !os.IsExist(err) {
			return nil, fmt.Errorf("create database lock file: %w", err)
		}

		stale, staleErr := staleLockState(lockPath)
		if staleErr != nil {
			return nil, staleErr
		}
		if !stale {
			return nil, fmt.Errorf("%w: %s", ErrDatabaseLocked, lockPath)
		}

		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove stale lock file: %w", err)
		}
	}
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

func writeLockPID(lockFile *os.File, lockPath string) error {
	// Writing the PID makes the lock file more inspectable when users troubleshoot the DB folder.
	if _, err := lockFile.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		lockFile.Close()
		_ = os.Remove(lockPath)
		return fmt.Errorf("write lock file pid: %w", err)
	}

	if err := lockFile.Sync(); err != nil {
		lockFile.Close()
		_ = os.Remove(lockPath)
		return fmt.Errorf("sync lock file: %w", err)
	}

	return nil
}

func staleLockState(lockPath string) (bool, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("read existing lock file: %w", err)
	}

	pidText := strings.TrimSpace(string(data))
	if pidText == "" {
		return true, nil
	}

	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return true, nil
	}

	exists, err := processExistsForLock(pid)
	if err != nil {
		return false, fmt.Errorf("inspect lock file pid %d: %w", pid, err)
	}

	return !exists, nil
}
