package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Set stores or replaces a value for a key using a one-operation durable batch.
func (db *DB) Set(key, value string) error {
	return db.ApplyBatch([]BatchOperation{{
		Command: commandSet,
		Key:     key,
		Value:   value,
	}})
}

// Get loads a value lazily from the snapshot or segment file referenced by the in-memory index.
func (db *DB) Get(key string) (string, bool) {
	db.mu.RLock()
	entry, ok := db.index[key]
	db.mu.RUnlock()
	if !ok {
		return "", false
	}

	value, err := db.readValueForEntry(entry)
	if err != nil {
		return "", false
	}

	return value, true
}

// GetRecordMetadata returns metadata for one live key without loading every value into memory.
func (db *DB) GetRecordMetadata(key string) (EntryMetadata, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	entry, ok := db.index[key]
	if !ok {
		return EntryMetadata{}, false
	}

	return EntryMetadata{
		Key:          entry.Key,
		ValueSize:    entry.ValueSize,
		CreatedAt:    entry.CreatedAt,
		UpdatedAt:    entry.UpdatedAt,
		LastSequence: entry.LastSequence,
	}, true
}

// Delete records a tombstone for a key.
func (db *DB) Delete(key string) error {
	db.mu.RLock()
	_, exists := db.index[key]
	db.mu.RUnlock()
	if !exists {
		return ErrKeyNotFound
	}

	return db.ApplyBatch([]BatchOperation{{
		Command: commandDelete,
		Key:     key,
	}})
}

// ApplyBatch commits multiple SET/DELETE operations atomically as one durable frame.
func (db *DB) ApplyBatch(operations []BatchOperation) error {
	if len(operations) == 0 {
		return fmt.Errorf("batch must contain at least one operation")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	persisted := make([]persistedOperation, 0, len(operations))
	nextSequence := db.metadata.NextSequence
	type deleteMutation struct {
		key      string
		sequence uint64
	}
	deletes := make([]deleteMutation, 0)

	for _, operation := range operations {
		if err := validateKey(operation.Key); err != nil {
			return err
		}

		command := strings.ToUpper(strings.TrimSpace(operation.Command))
		switch command {
		case commandSet:
			now := time.Now().UTC()
			createdAt := now
			if previous, exists := db.index[operation.Key]; exists {
				createdAt = previous.CreatedAt
			}

			persisted = append(persisted, persistedOperation{
				Sequence:  nextSequence,
				Command:   commandSet,
				Key:       operation.Key,
				Value:     operation.Value,
				ValueSize: len(operation.Value),
				CreatedAt: createdAt,
				UpdatedAt: now,
			})
			nextSequence++
		case commandDelete:
			if _, exists := db.index[operation.Key]; !exists {
				return fmt.Errorf("%w: %s", ErrKeyNotFound, operation.Key)
			}

			now := time.Now().UTC()
			persisted = append(persisted, persistedOperation{
				Sequence:  nextSequence,
				Command:   commandDelete,
				Key:       operation.Key,
				CreatedAt: now,
				UpdatedAt: now,
			})
			deletes = append(deletes, deleteMutation{
				key:      operation.Key,
				sequence: nextSequence,
			})
			nextSequence++
		default:
			return fmt.Errorf("unsupported batch command %q", operation.Command)
		}
	}

	entries, err := db.appendMutationBatch(persisted)
	if err != nil {
		return fmt.Errorf("append mutation batch: %w", err)
	}

	db.metadata.NextSequence = nextSequence
	for _, entry := range entries {
		db.index[entry.Key] = entry
		db.metadata.TotalSetOperations++
	}

	for _, deletion := range deletes {
		delete(db.index, deletion.key)
		db.metadata.TotalDeleteOps++
		_ = deletion.sequence
	}

	if err := db.saveMetadata(); err != nil {
		return fmt.Errorf("persist metadata after batch: %w", err)
	}

	return nil
}

// Keys lists live keys in sorted order with optional prefix filtering.
func (db *DB) Keys(prefix string) []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keys := make([]string, 0, len(db.index))
	for key := range db.index {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)
	return keys
}

// Records returns explorer rows with lazy value preview loading and optional pagination.
func (db *DB) Records(prefix string, offset int, limit int) []Record {
	db.mu.RLock()
	keys := make([]string, 0, len(db.index))
	for key := range db.index {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(keys)
	}
	if offset >= len(keys) {
		db.mu.RUnlock()
		return []Record{}
	}

	end := offset + limit
	if end > len(keys) {
		end = len(keys)
	}

	selectedKeys := append([]string(nil), keys[offset:end]...)
	entries := make(map[string]indexEntry, len(selectedKeys))
	for _, key := range selectedKeys {
		entries[key] = db.index[key]
	}
	db.mu.RUnlock()

	records := make([]Record, 0, len(selectedKeys))
	for _, key := range selectedKeys {
		entry := entries[key]
		value, err := db.readValueForEntry(entry)
		if err != nil {
			value = "<unavailable: " + err.Error() + ">"
		}

		records = append(records, Record{
			Key:          key,
			ValuePreview: valuePreview(value),
			ValueSize:    entry.ValueSize,
			CreatedAt:    entry.CreatedAt,
			UpdatedAt:    entry.UpdatedAt,
		})
	}

	return records
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrEmptyKey
	}

	return nil
}

// Execute parses and runs one MiniDB v2 console command.
func (db *DB) Execute(input string) (string, error) {
	commandText := strings.TrimSpace(input)
	if commandText == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	upper := strings.ToUpper(commandText)
	switch {
	case strings.HasPrefix(upper, "SET "):
		key, value, err := parseSetCommand(commandText)
		if err != nil {
			return "", err
		}
		if err := db.Set(key, value); err != nil {
			return "", err
		}
		return "OK", nil
	case strings.HasPrefix(upper, "GET "):
		key := strings.TrimSpace(commandText[4:])
		value, ok := db.Get(key)
		if !ok {
			return "", ErrKeyNotFound
		}
		return value, nil
	case strings.HasPrefix(upper, "DELETE "):
		key := strings.TrimSpace(commandText[7:])
		if err := db.Delete(key); err != nil {
			return "", err
		}
		return "OK", nil
	case upper == "KEYS":
		return strings.Join(db.Keys(""), "\n"), nil
	case strings.HasPrefix(upper, "KEYS "):
		prefix := strings.TrimSpace(commandText[5:])
		return strings.Join(db.Keys(prefix), "\n"), nil
	case upper == "STATS":
		stats, err := db.Stats()
		if err != nil {
			return "", err
		}
		return formatStats(stats), nil
	case upper == "COMPACT":
		if err := db.Compact(); err != nil {
			return "", err
		}
		return "OK", nil
	case upper == "SNAPSHOT":
		if err := db.Snapshot(); err != nil {
			return "", err
		}
		return "OK", nil
	case upper == "VALIDATE":
		report, err := db.Validate()
		if err != nil {
			return "", err
		}
		return formatValidation(report), nil
	case strings.HasPrefix(upper, "BATCH"):
		operations, err := parseBatchCommand(commandText)
		if err != nil {
			return "", err
		}
		if err := db.ApplyBatch(operations); err != nil {
			return "", err
		}
		return "OK", nil
	default:
		return "", fmt.Errorf("unsupported command: %s", commandText)
	}
}

func parseSetCommand(commandText string) (string, string, error) {
	rest := strings.TrimSpace(commandText[4:])
	firstSpace := strings.IndexAny(rest, " \t")
	if firstSpace == -1 {
		return "", "", fmt.Errorf("SET requires both key and value")
	}

	key := strings.TrimSpace(rest[:firstSpace])
	value := strings.TrimLeft(rest[firstSpace+1:], " \t")
	if err := validateKey(key); err != nil {
		return "", "", err
	}

	return key, value, nil
}

func parseBatchCommand(commandText string) ([]BatchOperation, error) {
	lines := strings.Split(strings.ReplaceAll(commandText, "\r\n", "\n"), "\n")
	if len(lines) < 2 || strings.ToUpper(strings.TrimSpace(lines[0])) != "BATCH" {
		return nil, fmt.Errorf("BATCH must start with a line containing only BATCH")
	}

	operations := make([]BatchOperation, 0, len(lines))
	for _, rawLine := range lines[1:] {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.ToUpper(line) == "END" {
			if len(operations) == 0 {
				return nil, fmt.Errorf("BATCH must contain at least one operation")
			}
			return operations, nil
		}

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "SET "):
			key, value, err := parseSetCommand(line)
			if err != nil {
				return nil, err
			}
			operations = append(operations, BatchOperation{
				Command: commandSet,
				Key:     key,
				Value:   value,
			})
		case strings.HasPrefix(upper, "DELETE "):
			key := strings.TrimSpace(line[7:])
			if err := validateKey(key); err != nil {
				return nil, err
			}
			operations = append(operations, BatchOperation{
				Command: commandDelete,
				Key:     key,
			})
		default:
			return nil, fmt.Errorf("unsupported batch line: %s", line)
		}
	}

	return nil, fmt.Errorf("BATCH must end with END")
}

func formatStats(stats Stats) string {
	lines := []string{
		"live_key_count=" + strconv.Itoa(stats.LiveKeyCount),
		"total_live_data_size=" + strconv.FormatInt(stats.TotalLiveDataSize, 10),
		"active_segment_size=" + strconv.FormatInt(stats.ActiveSegmentSize, 10),
		"segment_count=" + strconv.Itoa(stats.SegmentCount),
		"snapshot_count=" + strconv.Itoa(stats.SnapshotCount),
		"total_set_operations=" + strconv.FormatUint(stats.TotalSetOperations, 10),
		"total_delete_operations=" + strconv.FormatUint(stats.TotalDeleteOps, 10),
		"last_compaction_time=" + stats.LastCompactionTime.Format(time.RFC3339),
		"last_snapshot_time=" + stats.LastSnapshotTime.Format(time.RFC3339),
		"startup_replay_count=" + strconv.FormatUint(stats.StartupReplayCount, 10),
	}

	return strings.Join(lines, "\n")
}

func formatValidation(report ValidationReport) string {
	lines := []string{
		"snapshot_present=" + strconv.FormatBool(report.SnapshotPresent),
		"snapshot_valid=" + strconv.FormatBool(report.SnapshotValid),
		"snapshot_sequence=" + strconv.FormatUint(report.SnapshotSequence, 10),
		"segment_count=" + strconv.Itoa(report.SegmentCount),
		"valid_segments=" + strings.Join(report.ValidSegments, ","),
		"incomplete_tail_segments=" + strings.Join(report.IncompleteTailSegments, ","),
		"corrupted_segments=" + strings.Join(report.CorruptedSegments, ","),
	}
	if len(report.Warnings) > 0 {
		lines = append(lines, "warnings="+strings.Join(report.Warnings, " | "))
	}

	return strings.Join(lines, "\n")
}

func valuePreview(value string) string {
	flat := strings.ReplaceAll(value, "\r\n", "\n")
	flat = strings.ReplaceAll(flat, "\n", "\\n")
	if len(flat) <= 80 {
		return flat
	}

	return flat[:80] + "..."
}
