package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (db *DB) Snapshot() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.writeSnapshotLocked()
}

func (db *DB) writeSnapshotLocked() error {
	tempPath := filepath.Join(db.dataDir, "snapshot.dat.tmp")
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create snapshot temp file: %w", err)
	}

	header := snapshotHeader{
		Kind:         payloadKindSnapshotHead,
		LastSequence: db.currentSequence(),
		CreatedAt:    time.Now().UTC(),
	}

	if _, err := appendJSONFrame(tempFile, header); err != nil {
		tempFile.Close()
		return fmt.Errorf("write snapshot header: %w", err)
	}

	keys := make([]string, 0, len(db.index))
	for key := range db.index {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		entry := db.index[key]
		value, err := db.readValueForEntry(entry)
		if err != nil {
			tempFile.Close()
			return fmt.Errorf("load value for snapshot key %q: %w", key, err)
		}

		item := snapshotEntry{
			Kind:       payloadKindSnapshotItem,
			Sequence:   entry.LastSequence,
			Collection: entry.Collection,
			Key:        entry.Key,
			Value:      value,
			ValueKind:  entry.ValueKind,
			ValueSize:  entry.ValueSize,
			CreatedAt:  entry.CreatedAt,
			UpdatedAt:  entry.UpdatedAt,
		}

		if _, err := appendJSONFrame(tempFile, item); err != nil {
			tempFile.Close()
			return fmt.Errorf("write snapshot entry %q: %w", key, err)
		}
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return fmt.Errorf("sync snapshot temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close snapshot temp file: %w", err)
	}

	if err := os.Rename(tempPath, db.snapshotPath); err != nil {
		return fmt.Errorf("replace snapshot file: %w", err)
	}

	db.metadata.LastSnapshotTime = header.CreatedAt
	db.metadata.LastSnapshotSeq = header.LastSequence
	return db.saveMetadata()
}

func (db *DB) loadSnapshotLocked() (uint64, error) {
	file, err := os.Open(db.snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			db.metadata.LastSnapshotSeq = 0
			return 0, nil
		}

		return 0, fmt.Errorf("open snapshot file: %w", err)
	}
	defer file.Close()

	reader, err := newFrameReader(file, false)
	if err != nil {
		return 0, err
	}

	headerFrame, err := reader.next()
	if err != nil {
		return 0, fmt.Errorf("read snapshot header: %w", err)
	}

	var header snapshotHeader
	if err := json.Unmarshal(headerFrame.Payload, &header); err != nil {
		return 0, fmt.Errorf("decode snapshot header: %w", err)
	}
	if header.Kind != payloadKindSnapshotHead {
		return 0, fmt.Errorf("invalid snapshot header kind %q", header.Kind)
	}

	index := make(map[string]indexEntry)
	for {
		frame, err := reader.next()
		if err != nil {
			if err == os.ErrClosed || err.Error() == "EOF" {
				break
			}
			if err == io.EOF {
				break
			}
			return 0, fmt.Errorf("read snapshot entry: %w", err)
		}

		var item snapshotEntry
		if err := json.Unmarshal(frame.Payload, &item); err != nil {
			return 0, fmt.Errorf("decode snapshot entry: %w", err)
		}
		if item.Kind != payloadKindSnapshotItem {
			return 0, fmt.Errorf("invalid snapshot entry kind %q", item.Kind)
		}

		collection := normalizeCollection(item.Collection)
		entryKey := item.Key
		if item.Collection == "" && strings.Contains(item.Key, "/") {
			collection, entryKey = splitCanonicalKey(item.Key)
		}

		jsonFields := map[string]string(nil)
		if normalizeValueKind(item.ValueKind) == ValueKindJSON {
			jsonFields, err = extractIndexedJSONFields(item.Value)
			if err != nil {
				return 0, fmt.Errorf("index snapshot json fields for %s/%s: %w", collection, entryKey, err)
			}
		}

		index[canonicalKey(collection, entryKey)] = indexEntry{
			CanonicalKey: canonicalKey(collection, entryKey),
			Collection:   collection,
			Key:          entryKey,
			ValueKind:    normalizeValueKind(item.ValueKind),
			JSONFields:   jsonFields,
			SourceType:   sourceTypeSnapshot,
			SourcePath:   db.snapshotPath,
			FrameOffset:  frame.Offset,
			OperationIdx: 0,
			ValueSize:    item.ValueSize,
			CreatedAt:    item.CreatedAt,
			UpdatedAt:    item.UpdatedAt,
			LastSequence: item.Sequence,
		}
	}

	db.index = index
	db.rebuildDerivedIndexesLocked()
	db.metadata.LastSnapshotSeq = header.LastSequence
	if db.metadata.LastSnapshotTime.IsZero() {
		db.metadata.LastSnapshotTime = header.CreatedAt
	}

	return header.LastSequence, nil
}
