package engine

import (
	"fmt"
	"os"
	"time"
)

// Stats describes the current state of the database and its durable log.
type Stats struct {
	LiveKeyCount       int
	LogFileSize        int64
	TotalSetOperations uint64
	TotalDeleteOps     uint64
	LastCompactionTime time.Time
}

type metadata struct {
	LastCompactionTime  time.Time `json:"last_compaction_time"`
	TotalSetOperations  uint64    `json:"total_set_operations"`
	TotalDeleteOps      uint64    `json:"total_delete_operations"`
	SnapshotRecordCount uint64    `json:"snapshot_record_count"`
}

func (db *DB) loadMetadata() error {
	data, err := os.ReadFile(db.metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			db.lastCompactionTime = time.Time{}
			return nil
		}

		return fmt.Errorf("read metadata file: %w", err)
	}

	meta, err := decodeMetadata(data)
	if err != nil {
		return err
	}

	db.lastCompactionTime = meta.LastCompactionTime
	db.setOps = meta.TotalSetOperations
	db.deleteOps = meta.TotalDeleteOps
	db.snapshotRecordCount = meta.SnapshotRecordCount
	return nil
}

func (db *DB) saveMetadata() error {
	meta := metadata{
		LastCompactionTime:  db.lastCompactionTime,
		TotalSetOperations:  db.setOps,
		TotalDeleteOps:      db.deleteOps,
		SnapshotRecordCount: db.snapshotRecordCount,
	}

	data, err := encodeMetadata(meta)
	if err != nil {
		return err
	}

	if err := os.WriteFile(db.metadataPath, data, 0o644); err != nil {
		return fmt.Errorf("write metadata file: %w", err)
	}

	return nil
}

// Stats returns a snapshot of database counters and file size information.
func (db *DB) Stats() (Stats, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	info, err := db.logFile.Stat()
	if err != nil {
		return Stats{}, fmt.Errorf("stat log file: %w", err)
	}

	return Stats{
		LiveKeyCount:       len(db.index),
		LogFileSize:        info.Size(),
		TotalSetOperations: db.setOps,
		TotalDeleteOps:     db.deleteOps,
		LastCompactionTime: db.lastCompactionTime,
	}, nil
}
