package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RepairTo salvages as many valid records as possible into a fresh database directory.
// The source database remains untouched.
func (db *DB) RepairTo(destinationDir string) (RepairReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return RepairReport{}, fmt.Errorf("create repair destination %q: %w", destinationDir, err)
	}

	salvaged, report, err := db.collectSalvageState()
	if err != nil {
		return RepairReport{}, err
	}

	repairedDB, err := OpenInDirWithOptions(destinationDir, db.options)
	if err != nil {
		return RepairReport{}, fmt.Errorf("open repaired database: %w", err)
	}
	defer repairedDB.Close()

	keys := make([]string, 0, len(salvaged))
	for canonical := range salvaged {
		keys = append(keys, canonical)
	}
	sort.Strings(keys)

	for _, canonical := range keys {
		record := salvaged[canonical]
		if err := repairedDB.SetTypedInCollection(record.Collection, record.Key, record.Value, record.ValueKind); err != nil {
			return RepairReport{}, fmt.Errorf("write repaired record %q: %w", canonical, err)
		}
	}

	report.DestinationDir = destinationDir
	report.RecoveredRecords = len(keys)
	return report, nil
}

type salvageRecord struct {
	Collection string
	Key        string
	Value      string
	ValueKind  string
}

func (db *DB) collectSalvageState() (map[string]salvageRecord, RepairReport, error) {
	report := RepairReport{
		Warnings: []string{},
	}
	state := make(map[string]salvageRecord)

	if _, err := os.Stat(db.snapshotPath); err == nil {
		header, snapshotState, err := salvageSnapshot(db.snapshotPath)
		if err != nil {
			report.Warnings = append(report.Warnings, "snapshot skipped: "+err.Error())
		} else {
			report.UsedSnapshot = true
			_ = header
			for key, record := range snapshotState {
				state[key] = record
			}
		}
	}

	segments, err := db.segmentPaths()
	if err != nil {
		return nil, report, err
	}

	for _, path := range segments {
		if err := salvageSegmentIntoState(path, state, &report); err != nil {
			return nil, report, err
		}
	}

	return state, report, nil
}

func salvageSnapshot(path string) (snapshotHeader, map[string]salvageRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return snapshotHeader{}, nil, fmt.Errorf("open snapshot for repair: %w", err)
	}
	defer file.Close()

	reader, err := newFrameReader(file, false)
	if err != nil {
		return snapshotHeader{}, nil, err
	}

	headerFrame, err := reader.next()
	if err != nil {
		return snapshotHeader{}, nil, err
	}

	var header snapshotHeader
	if err := json.Unmarshal(headerFrame.Payload, &header); err != nil {
		return snapshotHeader{}, nil, fmt.Errorf("decode snapshot header: %w", err)
	}

	state := make(map[string]salvageRecord)
	for {
		frame, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return snapshotHeader{}, nil, err
		}

		var item snapshotEntry
		if err := json.Unmarshal(frame.Payload, &item); err != nil {
			return snapshotHeader{}, nil, fmt.Errorf("decode snapshot entry: %w", err)
		}

		collection := normalizeCollection(item.Collection)
		key := item.Key
		if item.Collection == "" && strings.Contains(item.Key, "/") {
			collection, key = splitCanonicalKey(item.Key)
		}

		state[canonicalKey(collection, key)] = salvageRecord{
			Collection: collection,
			Key:        key,
			Value:      item.Value,
			ValueKind:  normalizeValueKind(item.ValueKind),
		}
	}

	return header, state, nil
}

func salvageSegmentIntoState(path string, state map[string]salvageRecord, report *RepairReport) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open segment %q for repair: %w", path, err)
	}
	defer file.Close()

	reader, err := newFrameReader(file, true)
	if err != nil {
		return err
	}

	for {
		frame, err := reader.next()
		if err == io.EOF {
			if reader.offset < reader.fileSize {
				report.Warnings = append(report.Warnings, fmt.Sprintf("stopped at incomplete tail in %s", filepath.Base(path)))
			}
			break
		}
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("skipped remainder of %s after corruption: %v", filepath.Base(path), err))
			break
		}

		batch, err := decodeMutationBatch(frame)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("skipped invalid batch in %s: %v", filepath.Base(path), err))
			break
		}

		for _, operation := range batch.Operations {
			collection := normalizeCollection(operation.Collection)
			key := operation.Key
			if operation.Collection == "" && strings.Contains(operation.Key, "/") {
				collection, key = splitCanonicalKey(operation.Key)
			}
			canonical := canonicalKey(collection, key)

			switch operation.Command {
			case commandSet:
				state[canonical] = salvageRecord{
					Collection: collection,
					Key:        key,
					Value:      operation.Value,
					ValueKind:  normalizeValueKind(operation.ValueKind),
				}
			case commandDelete:
				delete(state, canonical)
			}
		}
	}

	return nil
}
