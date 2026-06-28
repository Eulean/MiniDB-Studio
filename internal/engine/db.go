package engine

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"minidb-studio/internal/storage"
)

var (
	// ErrEmptyKey prevents ambiguous writes and deletes against blank keys.
	ErrEmptyKey = errors.New("key must not be empty")
	// ErrKeyNotFound makes delete failures explicit to the caller.
	ErrKeyNotFound = errors.New("key not found")
	// ErrDatabaseLocked reports that another MiniDB process already owns the same DB directory.
	ErrDatabaseLocked = errors.New("database directory is already locked by another MiniDB process")
)

// DB owns the MiniDB v2 storage engine.
// It coordinates:
// - one active writable segment
// - an offset-based in-memory index
// - optional snapshot state
// - metadata counters and timestamps
// - a process-local lock file
type DB struct {
	mu sync.RWMutex

	dataDir      string
	metadataPath string
	snapshotPath string
	lock         *lockHandle
	options      OpenOptions

	activeSegmentID uint64
	activeSegment   *os.File
	index           map[string]indexEntry
	metadata        metadata
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
	return OpenInDirWithOptions(dataDir, OpenOptions{})
}

// OpenInDirWithOptions opens the database using caller-supplied storage options.
func OpenInDirWithOptions(dataDir string, options OpenOptions) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create database directory %q: %w", dataDir, err)
	}

	lock, err := acquireLock(dataDir)
	if err != nil {
		return nil, err
	}

	db := &DB{
		dataDir:      dataDir,
		metadataPath: filepath.Join(dataDir, "minidb.meta.json"),
		snapshotPath: filepath.Join(dataDir, "snapshot.dat"),
		lock:         lock,
		options:      options.withDefaults(),
		index:        make(map[string]indexEntry),
	}

	if err := db.migrateLegacyV1Files(); err != nil {
		lock.release()
		return nil, err
	}

	if err := db.loadMetadata(); err != nil {
		lock.release()
		return nil, err
	}

	if err := db.openOrCreateActiveSegment(); err != nil {
		lock.release()
		return nil, err
	}

	if err := db.recoverState(); err != nil {
		db.activeSegment.Close()
		lock.release()
		return nil, err
	}

	if err := db.saveMetadata(); err != nil {
		db.activeSegment.Close()
		lock.release()
		return nil, err
	}

	return db, nil
}

// Close releases the log file handle.
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.activeSegment != nil {
		if err := db.activeSegment.Close(); err != nil {
			return fmt.Errorf("close active segment: %w", err)
		}
		db.activeSegment = nil
	}

	if err := db.lock.release(); err != nil {
		return err
	}

	return nil
}

// DataDir exposes the active storage directory for the UI status bar and backup features.
func (db *DB) DataDir() string {
	return db.dataDir
}

// BackupTo writes a zip archive containing metadata, snapshot, and segment files.
func (db *DB) BackupTo(destinationPath string) error {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if err := db.activeSegment.Sync(); err != nil {
		return fmt.Errorf("sync active segment before backup: %w", err)
	}

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create backup archive %q: %w", destinationPath, err)
	}
	defer destinationFile.Close()

	archive := zip.NewWriter(destinationFile)
	files := []string{db.metadataPath}
	if _, err := os.Stat(db.snapshotPath); err == nil {
		files = append(files, db.snapshotPath)
	}
	segments, err := db.segmentPaths()
	if err != nil {
		archive.Close()
		return err
	}
	files = append(files, segments...)
	sort.Strings(files)

	for _, sourcePath := range files {
		if err := addFileToZip(archive, sourcePath, filepath.Base(sourcePath)); err != nil {
			archive.Close()
			return err
		}
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("close backup archive: %w", err)
	}

	if err := destinationFile.Sync(); err != nil {
		return fmt.Errorf("sync backup archive: %w", err)
	}

	return nil
}

func addFileToZip(archive *zip.Writer, sourcePath, archiveName string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open %q for backup: %w", sourcePath, err)
	}
	defer sourceFile.Close()

	writer, err := archive.Create(archiveName)
	if err != nil {
		return fmt.Errorf("create archive entry %q: %w", archiveName, err)
	}

	if _, err := io.Copy(writer, sourceFile); err != nil {
		return fmt.Errorf("copy %q into archive: %w", sourcePath, err)
	}

	return nil
}

// migrateLegacyV1Files preserves existing user data by promoting the old single-log layout into segment 1.
func (db *DB) migrateLegacyV1Files() error {
	legacyLogPath := filepath.Join(db.dataDir, "minidb.log")
	if _, err := os.Stat(legacyLogPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("stat legacy v1 log: %w", err)
	}

	segmentPaths, err := db.segmentPaths()
	if err != nil {
		return err
	}
	if len(segmentPaths) > 0 {
		return nil
	}

	targetPath := segmentPath(db.dataDir, 1)
	if err := os.Rename(legacyLogPath, targetPath); err != nil {
		return fmt.Errorf("migrate legacy v1 log to segment 1: %w", err)
	}

	if db.metadata.NextSegmentID < 2 {
		db.metadata.NextSegmentID = 2
	}
	if db.metadata.ActiveSegmentID == 0 {
		db.metadata.ActiveSegmentID = 1
	}

	return nil
}
