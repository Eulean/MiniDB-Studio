package engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"minidb-studio/internal/storage"
)

var (
	// ErrEmptyKey prevents ambiguous writes and deletes against blank keys.
	ErrEmptyKey = errors.New("key must not be empty")
	// ErrKeyNotFound makes delete failures explicit to the caller.
	ErrKeyNotFound = errors.New("key not found")
)

// DB owns the append-only log, in-memory index, and statistics counters.
// A single process can safely access it concurrently through the RWMutex.
type DB struct {
	mu sync.RWMutex

	dataDir      string
	logPath      string
	metadataPath string
	logFile      *os.File

	index map[string]string

	setOps              uint64
	deleteOps           uint64
	lastCompactionTime  time.Time
	snapshotRecordCount uint64
}

// Record is the UI-friendly view of a live key-value entry.
type Record struct {
	Key   string
	Value string
}

// Open opens or creates the database in the default MiniDB Studio app-data folder.
func Open() (*DB, error) {
	dataDir, err := storage.EnsureAppDataDir()
	if err != nil {
		return nil, err
	}

	return OpenInDir(dataDir)
}

// OpenInDir is primarily useful for tests and for future UI wiring.
func OpenInDir(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create database directory %q: %w", dataDir, err)
	}

	logPath := filepath.Join(dataDir, "minidb.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", logPath, err)
	}

	db := &DB{
		dataDir:      dataDir,
		logPath:      logPath,
		metadataPath: filepath.Join(dataDir, "minidb.meta.json"),
		logFile:      logFile,
		index:        make(map[string]string),
	}

	if err := db.loadMetadata(); err != nil {
		logFile.Close()
		return nil, err
	}

	if err := db.replayLog(); err != nil {
		logFile.Close()
		return nil, err
	}

	return db, nil
}

// Close releases the log file handle.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.logFile == nil {
		return nil
	}

	if err := db.logFile.Close(); err != nil {
		return fmt.Errorf("close log file: %w", err)
	}

	db.logFile = nil
	return nil
}

// DataDir exposes the active storage directory for the UI status bar and backup features.
func (db *DB) DataDir() string {
	return db.dataDir
}

// BackupTo copies the current durable log to a caller-chosen destination.
func (db *DB) BackupTo(destinationPath string) error {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if err := db.logFile.Sync(); err != nil {
		return fmt.Errorf("sync log before backup: %w", err)
	}

	sourceFile, err := os.Open(db.logPath)
	if err != nil {
		return fmt.Errorf("open source log for backup: %w", err)
	}
	defer sourceFile.Close()

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create backup file %q: %w", destinationPath, err)
	}
	defer destinationFile.Close()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return fmt.Errorf("copy log to backup: %w", err)
	}

	if err := destinationFile.Sync(); err != nil {
		return fmt.Errorf("sync backup file: %w", err)
	}

	return nil
}
