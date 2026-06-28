package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Validate inspects the current snapshot and segment files without mutating the database.
func (db *DB) Validate() (ValidationReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	report := ValidationReport{
		ValidSegments:          []string{},
		IncompleteTailSegments: []string{},
		CorruptedSegments:      []string{},
		Warnings:               []string{},
	}

	if _, err := os.Stat(db.snapshotPath); err == nil {
		report.SnapshotPresent = true
		header, err := validateSnapshotFile(db.snapshotPath)
		if err != nil {
			report.SnapshotValid = false
			report.CorruptedSegments = append(report.CorruptedSegments, filepath.Base(db.snapshotPath))
			report.Warnings = append(report.Warnings, err.Error())
		} else {
			report.SnapshotValid = true
			report.SnapshotSequence = header.LastSequence
		}
	}

	segments, err := db.segmentPaths()
	if err != nil {
		return report, err
	}
	report.SegmentCount = len(segments)

	for _, path := range segments {
		status, err := validateSegmentFile(path)
		switch {
		case err != nil:
			report.CorruptedSegments = append(report.CorruptedSegments, filepath.Base(path))
			report.Warnings = append(report.Warnings, err.Error())
		case status == "incomplete_tail":
			report.IncompleteTailSegments = append(report.IncompleteTailSegments, filepath.Base(path))
		default:
			report.ValidSegments = append(report.ValidSegments, filepath.Base(path))
		}
	}

	return report, nil
}

func validateSnapshotFile(path string) (snapshotHeader, error) {
	file, err := os.Open(path)
	if err != nil {
		return snapshotHeader{}, fmt.Errorf("open snapshot for validation: %w", err)
	}
	defer file.Close()

	reader, err := newFrameReader(file, false)
	if err != nil {
		return snapshotHeader{}, err
	}

	headerFrame, err := reader.next()
	if err != nil {
		return snapshotHeader{}, err
	}

	var header snapshotHeader
	if err := json.Unmarshal(headerFrame.Payload, &header); err != nil {
		return snapshotHeader{}, fmt.Errorf("decode snapshot header: %w", err)
	}
	if header.Kind != payloadKindSnapshotHead {
		return snapshotHeader{}, fmt.Errorf("invalid snapshot header kind %q", header.Kind)
	}

	for {
		frame, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return snapshotHeader{}, err
		}

		var item snapshotEntry
		if err := json.Unmarshal(frame.Payload, &item); err != nil {
			return snapshotHeader{}, fmt.Errorf("decode snapshot entry at %d: %w", frame.Offset, err)
		}
		if item.Kind != payloadKindSnapshotItem {
			return snapshotHeader{}, fmt.Errorf("invalid snapshot entry kind %q", item.Kind)
		}
	}

	return header, nil
}

func validateSegmentFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open segment %q for validation: %w", path, err)
	}
	defer file.Close()

	reader, err := newFrameReader(file, false)
	if err != nil {
		return "", err
	}

	for {
		frame, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if containsInterruptedWrite(err) {
				return "incomplete_tail", nil
			}
			return "", err
		}

		if _, err := decodeMutationBatch(frame); err != nil {
			return "", fmt.Errorf("segment %q contains invalid batch: %w", path, err)
		}
	}

	return "valid", nil
}

func containsInterruptedWrite(err error) bool {
	message := err.Error()
	return strings.Contains(message, "read frame header") || strings.Contains(message, "read frame payload") || strings.Contains(message, "read frame checksum")
}
