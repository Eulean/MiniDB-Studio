package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Compact rewrites only live records into a fresh log, swaps it in safely, and reloads state.
func (db *DB) Compact() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	tempPath := filepath.Join(db.dataDir, "minidb.log.compacting")
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create compacted log: %w", err)
	}

	keys := make([]string, 0, len(db.index))
	for key := range db.index {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := db.index[key]
		record := logRecord{
			Command:   commandSet,
			Key:       key,
			Value:     value,
			Timestamp: time.Now().UTC(),
		}

		if err := appendRecord(tempFile, record); err != nil {
			tempFile.Close()
			return fmt.Errorf("write compacted record for %q: %w", key, err)
		}
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("sync compacted log: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close compacted log: %w", err)
	}

	if err := db.logFile.Close(); err != nil {
		return fmt.Errorf("close original log before swap: %w", err)
	}

	backupPath := filepath.Join(db.dataDir, "minidb.log.pre-compact")
	_ = os.Remove(backupPath)
	if err := os.Rename(db.logPath, backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("move original log before swap: %w", err)
	}

	if err := os.Rename(tempPath, db.logPath); err != nil {
		return fmt.Errorf("replace log with compacted version: %w", err)
	}
	_ = os.Remove(backupPath)

	logFile, err := os.OpenFile(db.logPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("reopen log after compaction: %w", err)
	}
	db.logFile = logFile

	db.lastCompactionTime = time.Now().UTC()
	db.snapshotRecordCount = uint64(len(keys))
	if err := db.saveMetadata(); err != nil {
		return err
	}

	if err := db.replayLog(); err != nil {
		return fmt.Errorf("reload state after compaction: %w", err)
	}

	return nil
}
