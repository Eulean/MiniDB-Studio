package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

func (db *DB) loadMetadata() error {
	data, err := os.ReadFile(db.metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			db.metadata = metadata{
				NextSegmentID: 1,
				NextSequence:  1,
			}
			return nil
		}

		return fmt.Errorf("read metadata file: %w", err)
	}

	meta, err := decodeMetadata(data)
	if err != nil {
		return err
	}

	if meta.NextSegmentID == 0 {
		meta.NextSegmentID = 1
	}
	if meta.NextSequence == 0 {
		meta.NextSequence = 1
	}

	db.metadata = meta
	return nil
}

func (db *DB) saveMetadata() error {
	data, err := encodeMetadata(db.metadata)
	if err != nil {
		return err
	}

	tempPath := db.metadataPath + ".tmp"
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create metadata temp file: %w", err)
	}

	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("write metadata temp file: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("sync metadata temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close metadata temp file: %w", err)
	}

	if err := os.Rename(tempPath, db.metadataPath); err != nil {
		return fmt.Errorf("replace metadata file: %w", err)
	}

	return nil
}

// Stats returns a snapshot of the current storage layout and counters.
func (db *DB) Stats() (Stats, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	segmentPaths, err := db.segmentPaths()
	if err != nil {
		return Stats{}, err
	}

	activeInfo, err := db.activeSegment.Stat()
	if err != nil {
		return Stats{}, fmt.Errorf("stat active segment: %w", err)
	}

	var totalLiveDataSize int64
	for _, entry := range db.index {
		totalLiveDataSize += int64(entry.ValueSize)
	}

	snapshotCount := 0
	if _, err := os.Stat(db.snapshotPath); err == nil {
		snapshotCount = 1
	}

	return Stats{
		LiveKeyCount:       len(db.index),
		TotalLiveDataSize:  totalLiveDataSize,
		ActiveSegmentSize:  activeInfo.Size(),
		SegmentCount:       len(segmentPaths),
		SnapshotCount:      snapshotCount,
		TotalSetOperations: db.metadata.TotalSetOperations,
		TotalDeleteOps:     db.metadata.TotalDeleteOps,
		LastCompactionTime: db.metadata.LastCompactionTime,
		LastSnapshotTime:   db.metadata.LastSnapshotTime,
		StartupReplayCount: db.metadata.StartupReplayCount,
	}, nil
}

func (db *DB) currentSequence() uint64 {
	if db.metadata.NextSequence == 0 {
		return 0
	}

	return db.metadata.NextSequence - 1
}

func (db *DB) snapshotExists() bool {
	_, err := os.Stat(db.snapshotPath)
	return err == nil
}

func (db *DB) snapshotArchiveName() string {
	return filepath.Base(db.snapshotPath)
}
