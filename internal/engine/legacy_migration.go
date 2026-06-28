package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const legacyFrameMagic = "MDB1"

type legacyRecord struct {
	Command   string    `json:"command"`
	Key       string    `json:"key"`
	Value     string    `json:"value,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// migrateLegacyV1Files upgrades older MDB1-on-disk layouts into the current segment format.
// We preserve the old files in a timestamped backup folder before the current engine starts replaying.
func (db *DB) migrateLegacyV1Files() error {
	legacyPaths, legacyDetected, err := db.legacyFramePaths()
	if err != nil {
		return err
	}
	if legacyDetected {
		return db.migrateLegacyFrameFiles(legacyPaths)
	}

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

func (db *DB) legacyFramePaths() ([]string, bool, error) {
	segmentPaths, err := db.segmentPaths()
	if err != nil {
		return nil, false, err
	}
	if len(segmentPaths) > 0 {
		legacy, err := fileStartsWithMagic(segmentPaths[0], legacyFrameMagic)
		if err != nil {
			return nil, false, err
		}
		if legacy {
			return segmentPaths, true, nil
		}
	}

	legacyLogPath := filepath.Join(db.dataDir, "minidb.log")
	if _, err := os.Stat(legacyLogPath); err == nil {
		legacy, legacyErr := fileStartsWithMagic(legacyLogPath, legacyFrameMagic)
		if legacyErr != nil {
			return nil, false, legacyErr
		}
		if legacy {
			return []string{legacyLogPath}, true, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, false, fmt.Errorf("stat legacy log path: %w", err)
	}

	return nil, false, nil
}

func fileStartsWithMagic(path, magic string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open %q for legacy detection: %w", path, err)
	}
	defer file.Close()

	header := make([]byte, len(magic))
	if _, err := file.Read(header); err != nil {
		return false, fmt.Errorf("read magic from %q: %w", path, err)
	}

	return string(header) == magic, nil
}

func (db *DB) migrateLegacyFrameFiles(paths []string) error {
	records, err := readLegacyRecords(paths)
	if err != nil {
		return err
	}

	tempSegmentPath := filepath.Join(db.dataDir, "segment-000001.log.migrating")
	tempSegment, err := os.OpenFile(tempSegmentPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create migrated segment: %w", err)
	}

	createdAtByKey := make(map[string]time.Time)
	var nextSequence uint64 = 1
	var totalSets uint64
	var totalDeletes uint64
	for _, record := range records {
		createdAt := record.Timestamp.UTC()
		if previous, exists := createdAtByKey[record.Key]; exists {
			createdAt = previous
		}

		operation := persistedOperation{
			Sequence:   nextSequence,
			Command:    record.Command,
			Collection: DefaultCollection,
			Key:        record.Key,
			Value:      record.Value,
			ValueKind:  ValueKindRaw,
			ValueSize:  len(record.Value),
			CreatedAt:  createdAt,
			UpdatedAt:  record.Timestamp.UTC(),
		}

		if record.Command == commandSet {
			createdAtByKey[record.Key] = createdAt
			totalSets++
		} else if record.Command == commandDelete {
			delete(createdAtByKey, record.Key)
			totalDeletes++
		}

		if _, err := appendJSONFrame(tempSegment, mutationBatch{
			Kind:        payloadKindMutation,
			CommittedAt: record.Timestamp.UTC(),
			Operations:  []persistedOperation{operation},
		}); err != nil {
			tempSegment.Close()
			return fmt.Errorf("write migrated legacy record %q: %w", record.Key, err)
		}

		nextSequence++
	}

	if err := tempSegment.Sync(); err != nil {
		tempSegment.Close()
		return fmt.Errorf("sync migrated segment: %w", err)
	}
	if err := tempSegment.Close(); err != nil {
		return fmt.Errorf("close migrated segment: %w", err)
	}

	backupDir := filepath.Join(db.dataDir, "legacy-backup-"+time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("create legacy backup directory: %w", err)
	}

	pathsToMove := append([]string(nil), paths...)
	if _, err := os.Stat(db.metadataPath); err == nil {
		pathsToMove = append(pathsToMove, db.metadataPath)
	}
	if _, err := os.Stat(db.snapshotPath); err == nil {
		pathsToMove = append(pathsToMove, db.snapshotPath)
	}
	sort.Strings(pathsToMove)

	for _, sourcePath := range pathsToMove {
		targetPath := filepath.Join(backupDir, filepath.Base(sourcePath))
		if err := os.Rename(sourcePath, targetPath); err != nil {
			return fmt.Errorf("move legacy file %q to backup: %w", sourcePath, err)
		}
	}

	finalSegmentPath := segmentPath(db.dataDir, 1)
	if err := os.Rename(tempSegmentPath, finalSegmentPath); err != nil {
		return fmt.Errorf("place migrated segment: %w", err)
	}

	meta := metadata{
		ActiveSegmentID:    1,
		NextSegmentID:      2,
		NextSequence:       nextSequence,
		TotalSetOperations: totalSets,
		TotalDeleteOps:     totalDeletes,
	}
	data, err := encodeMetadata(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(db.metadataPath, data, 0o644); err != nil {
		return fmt.Errorf("write migrated metadata: %w", err)
	}

	return nil
}

func readLegacyRecords(paths []string) ([]legacyRecord, error) {
	records := make([]legacyRecord, 0)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open legacy frame file %q: %w", path, err)
		}

		reader, err := newLegacyFrameReader(file)
		if err != nil {
			file.Close()
			return nil, err
		}

		for {
			frame, err := reader.next()
			if err == io.EOF {
				break
			}
			if err != nil {
				file.Close()
				return nil, fmt.Errorf("read legacy frame from %q: %w", path, err)
			}

			var record legacyRecord
			if err := json.Unmarshal(frame.Payload, &record); err != nil {
				file.Close()
				return nil, fmt.Errorf("decode legacy record from %q: %w", path, err)
			}
			records = append(records, record)
		}

		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close legacy frame file %q: %w", path, err)
		}
	}

	return records, nil
}
