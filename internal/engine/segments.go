package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func segmentPath(dataDir string, segmentID uint64) string {
	return filepath.Join(dataDir, fmt.Sprintf("segment-%06d.log", segmentID))
}

func parseSegmentID(path string) (uint64, error) {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "segment-") || !strings.HasSuffix(base, ".log") {
		return 0, fmt.Errorf("invalid segment filename %q", base)
	}

	idText := strings.TrimSuffix(strings.TrimPrefix(base, "segment-"), ".log")
	segmentID, err := strconv.ParseUint(idText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse segment id from %q: %w", base, err)
	}

	return segmentID, nil
}

func (db *DB) segmentPaths() ([]string, error) {
	entries, err := os.ReadDir(db.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read database directory: %w", err)
	}

	paths := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), "segment-") && strings.HasSuffix(entry.Name(), ".log") {
			paths = append(paths, filepath.Join(db.dataDir, entry.Name()))
		}
	}

	sort.Slice(paths, func(i, j int) bool {
		leftID, _ := parseSegmentID(paths[i])
		rightID, _ := parseSegmentID(paths[j])
		return leftID < rightID
	})

	return paths, nil
}

func (db *DB) openOrCreateActiveSegment() error {
	paths, err := db.segmentPaths()
	if err != nil {
		return err
	}

	if len(paths) == 0 {
		if db.metadata.NextSegmentID == 0 {
			db.metadata.NextSegmentID = 1
		}
		db.activeSegmentID = db.metadata.NextSegmentID
		db.metadata.ActiveSegmentID = db.activeSegmentID
		db.metadata.NextSegmentID++
	} else {
		lastSegmentID, err := parseSegmentID(paths[len(paths)-1])
		if err != nil {
			return err
		}
		db.activeSegmentID = lastSegmentID
		db.metadata.ActiveSegmentID = lastSegmentID
		if db.metadata.NextSegmentID <= lastSegmentID {
			db.metadata.NextSegmentID = lastSegmentID + 1
		}
	}

	activePath := segmentPath(db.dataDir, db.activeSegmentID)
	file, err := os.OpenFile(activePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open active segment %q: %w", activePath, err)
	}

	db.activeSegment = file
	return nil
}

func (db *DB) rotateActiveSegmentIfNeeded() error {
	info, err := db.activeSegment.Stat()
	if err != nil {
		return fmt.Errorf("stat active segment: %w", err)
	}

	if info.Size() < db.options.SegmentSizeLimit {
		return nil
	}

	if err := db.activeSegment.Close(); err != nil {
		return fmt.Errorf("close active segment during rotation: %w", err)
	}

	db.activeSegmentID = db.metadata.NextSegmentID
	db.metadata.ActiveSegmentID = db.activeSegmentID
	db.metadata.NextSegmentID++

	activePath := segmentPath(db.dataDir, db.activeSegmentID)
	file, err := os.OpenFile(activePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open rotated segment %q: %w", activePath, err)
	}

	db.activeSegment = file
	return nil
}

func (db *DB) appendMutationBatch(operations []persistedOperation) ([]indexEntry, error) {
	if len(operations) == 0 {
		return nil, fmt.Errorf("mutation batch must contain at least one operation")
	}

	if err := db.rotateActiveSegmentIfNeeded(); err != nil {
		return nil, err
	}

	record := mutationBatch{
		Kind:        payloadKindMutation,
		CommittedAt: time.Now().UTC(),
		Operations:  operations,
	}

	frameOffset, err := appendJSONFrame(db.activeSegment, record)
	if err != nil {
		return nil, err
	}

	sourcePath := segmentPath(db.dataDir, db.activeSegmentID)
	entries := make([]indexEntry, 0, len(operations))
	for operationIndex, operation := range operations {
		if operation.Command != commandSet {
			continue
		}

		collection := normalizeCollection(operation.Collection)
		entries = append(entries, indexEntry{
			CanonicalKey: canonicalKey(collection, operation.Key),
			Collection:   collection,
			Key:          operation.Key,
			ValueKind:    normalizeValueKind(operation.ValueKind),
			SourceType:   sourceTypeSegment,
			SourcePath:   sourcePath,
			FrameOffset:  frameOffset,
			OperationIdx: operationIndex,
			ValueSize:    operation.ValueSize,
			CreatedAt:    operation.CreatedAt,
			UpdatedAt:    operation.UpdatedAt,
			LastSequence: operation.Sequence,
		})
	}

	return entries, nil
}

func (db *DB) readValueForEntry(entry indexEntry) (string, error) {
	switch entry.SourceType {
	case sourceTypeSegment:
		var batch mutationBatch
		if err := readJSONFrameAt(entry.SourcePath, entry.FrameOffset, &batch); err != nil {
			return "", err
		}

		if entry.OperationIdx < 0 || entry.OperationIdx >= len(batch.Operations) {
			return "", fmt.Errorf("operation index %d out of range for frame at %d", entry.OperationIdx, entry.FrameOffset)
		}

		return batch.Operations[entry.OperationIdx].Value, nil
	case sourceTypeSnapshot:
		var item snapshotEntry
		if err := readJSONFrameAt(entry.SourcePath, entry.FrameOffset, &item); err != nil {
			return "", err
		}

		return item.Value, nil
	default:
		return "", fmt.Errorf("unsupported entry source type %q", entry.SourceType)
	}
}

func decodeMutationBatch(frame framedPayload) (mutationBatch, error) {
	var batch mutationBatch
	if err := json.Unmarshal(frame.Payload, &batch); err != nil {
		return mutationBatch{}, fmt.Errorf("decode mutation batch: %w", err)
	}
	if batch.Kind != payloadKindMutation {
		return mutationBatch{}, fmt.Errorf("unexpected payload kind %q in segment", batch.Kind)
	}
	return batch, nil
}
