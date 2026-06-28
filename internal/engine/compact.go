package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Compact rewrites only live records into a fresh segment set and removes obsolete segments.
func (db *DB) Compact() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	keys := make([]string, 0, len(db.index))
	for key := range db.index {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return db.index[keys[i]].LastSequence < db.index[keys[j]].LastSequence
	})

	type compactedEntry struct {
		key   string
		entry indexEntry
		value string
	}

	liveEntries := make([]compactedEntry, 0, len(keys))
	for _, key := range keys {
		entry := db.index[key]
		value, err := db.readValueForEntry(entry)
		if err != nil {
			return fmt.Errorf("load value for compaction key %q: %w", key, err)
		}
		liveEntries = append(liveEntries, compactedEntry{
			key:   key,
			entry: entry,
			value: value,
		})
	}

	newSegmentID := db.metadata.NextSegmentID
	tempPath := filepath.Join(db.dataDir, fmt.Sprintf("segment-%06d.compacting", newSegmentID))
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create compacted segment: %w", err)
	}

	replacementIndex := make(map[string]indexEntry, len(liveEntries))
	segmentPathFinal := segmentPath(db.dataDir, newSegmentID)
	for _, item := range liveEntries {
		record := mutationBatch{
			Kind:        payloadKindMutation,
			CommittedAt: time.Now().UTC(),
			Operations: []persistedOperation{{
				Sequence:   item.entry.LastSequence,
				Command:    commandSet,
				Collection: item.entry.Collection,
				Key:        item.entry.Key,
				Value:      item.value,
				ValueKind:  item.entry.ValueKind,
				ValueSize:  item.entry.ValueSize,
				CreatedAt:  item.entry.CreatedAt,
				UpdatedAt:  item.entry.UpdatedAt,
			}},
		}

		offset, err := appendJSONFrame(tempFile, record)
		if err != nil {
			tempFile.Close()
			return fmt.Errorf("write compacted entry %q: %w", item.key, err)
		}

		replacementIndex[item.key] = indexEntry{
			CanonicalKey: item.entry.CanonicalKey,
			Collection:   item.entry.Collection,
			Key:          item.entry.Key,
			ValueKind:    item.entry.ValueKind,
			JSONFields:   item.entry.JSONFields,
			SourceType:   sourceTypeSegment,
			SourcePath:   segmentPathFinal,
			FrameOffset:  offset,
			OperationIdx: 0,
			ValueSize:    item.entry.ValueSize,
			CreatedAt:    item.entry.CreatedAt,
			UpdatedAt:    item.entry.UpdatedAt,
			LastSequence: item.entry.LastSequence,
		}
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("sync compacted segment: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close compacted segment: %w", err)
	}

	oldSegmentPaths, err := db.segmentPaths()
	if err != nil {
		return err
	}

	if err := db.activeSegment.Close(); err != nil {
		return fmt.Errorf("close active segment before compaction swap: %w", err)
	}

	if err := os.Rename(tempPath, segmentPathFinal); err != nil {
		return fmt.Errorf("rename compacted segment: %w", err)
	}

	for _, oldPath := range oldSegmentPaths {
		if oldPath == segmentPathFinal {
			continue
		}
		_ = os.Remove(oldPath)
	}

	// Compaction resets snapshot replay assumptions so the fresh segment set can replay from zero cleanly.
	_ = os.Remove(db.snapshotPath)
	db.metadata.LastSnapshotSeq = 0
	db.index = replacementIndex
	db.rebuildDerivedIndexesLocked()
	db.activeSegmentID = newSegmentID
	db.metadata.ActiveSegmentID = newSegmentID
	db.metadata.NextSegmentID = newSegmentID + 1
	db.metadata.LastCompactionTime = time.Now().UTC()

	file, err := os.OpenFile(segmentPathFinal, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("reopen compacted segment: %w", err)
	}
	db.activeSegment = file

	return db.saveMetadata()
}
