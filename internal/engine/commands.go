package engine

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Set stores or replaces a value for a key using a one-operation durable batch.
func (db *DB) Set(key, value string) error {
	return db.SetInCollection(DefaultCollection, key, value)
}

// SetInCollection stores or replaces a value in the requested collection.
func (db *DB) SetInCollection(collection, key, value string) error {
	return db.SetTypedInCollection(collection, key, value, ValueKindRaw)
}

// SetTypedInCollection stores a value with explicit value-kind metadata such as raw or json.
func (db *DB) SetTypedInCollection(collection, key, value, valueKind string) error {
	return db.ApplyBatch([]BatchOperation{{
		Command:    commandSet,
		Collection: collection,
		Key:        key,
		Value:      value,
		ValueKind:  valueKind,
	}})
}

// Get loads a value lazily from the snapshot or segment file referenced by the in-memory index.
func (db *DB) Get(key string) (string, bool) {
	return db.GetFromCollection(DefaultCollection, key)
}

// GetFromCollection loads a value lazily from the requested collection.
func (db *DB) GetFromCollection(collection, key string) (string, bool) {
	db.mu.RLock()
	entry, ok := db.index[canonicalKey(collection, key)]
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
	return db.GetRecordMetadataInCollection(DefaultCollection, key)
}

// GetRecordMetadataInCollection returns metadata for one live key in the requested collection.
func (db *DB) GetRecordMetadataInCollection(collection, key string) (EntryMetadata, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	entry, ok := db.index[canonicalKey(collection, key)]
	if !ok {
		return EntryMetadata{}, false
	}

	return EntryMetadata{
		Collection:   entry.Collection,
		Key:          entry.Key,
		ValueSize:    entry.ValueSize,
		ValueKind:    entry.ValueKind,
		CreatedAt:    entry.CreatedAt,
		UpdatedAt:    entry.UpdatedAt,
		LastSequence: entry.LastSequence,
	}, true
}

// Delete records a tombstone for a key.
func (db *DB) Delete(key string) error {
	return db.DeleteFromCollection(DefaultCollection, key)
}

// DeleteFromCollection removes a key from the requested collection.
func (db *DB) DeleteFromCollection(collection, key string) error {
	db.mu.RLock()
	_, exists := db.index[canonicalKey(collection, key)]
	db.mu.RUnlock()
	if !exists {
		return ErrKeyNotFound
	}

	return db.ApplyBatch([]BatchOperation{{
		Command:    commandDelete,
		Collection: collection,
		Key:        key,
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
		collection := normalizeCollection(operation.Collection)
		if err := validateCollection(collection); err != nil {
			return err
		}
		if err := validateKey(operation.Key); err != nil {
			return err
		}
		if err := validateValueKind(operation.ValueKind); err != nil {
			return err
		}

		command := strings.ToUpper(strings.TrimSpace(operation.Command))
		key := operation.Key
		compositeKey := canonicalKey(collection, key)
		switch command {
		case commandSet:
			if operation.ValueKind == "" {
				operation.ValueKind = ValueKindRaw
			}
			if operation.ValueKind == ValueKindJSON {
				if !json.Valid([]byte(operation.Value)) {
					return fmt.Errorf("invalid json value for %s/%s", collection, key)
				}
			}

			now := time.Now().UTC()
			createdAt := now
			if previous, exists := db.index[compositeKey]; exists {
				createdAt = previous.CreatedAt
			}

			persisted = append(persisted, persistedOperation{
				Sequence:   nextSequence,
				Command:    commandSet,
				Collection: collection,
				Key:        key,
				Value:      operation.Value,
				ValueKind:  operation.ValueKind,
				ValueSize:  len(operation.Value),
				CreatedAt:  createdAt,
				UpdatedAt:  now,
			})
			nextSequence++
		case commandDelete:
			if _, exists := db.index[compositeKey]; !exists {
				return fmt.Errorf("%w: %s/%s", ErrKeyNotFound, collection, key)
			}

			now := time.Now().UTC()
			persisted = append(persisted, persistedOperation{
				Sequence:   nextSequence,
				Command:    commandDelete,
				Collection: collection,
				Key:        key,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
			deletes = append(deletes, deleteMutation{
				key:      compositeKey,
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
		db.index[entry.CanonicalKey] = entry
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
	return db.KeysInCollection(DefaultCollection, prefix)
}

// KeysInCollection lists keys in one collection with optional prefix filtering.
func (db *DB) KeysInCollection(collection, prefix string) []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keys := make([]string, 0, len(db.index))
	collection = normalizeCollection(collection)
	for _, entry := range db.index {
		if entry.Collection != collection {
			continue
		}
		if prefix == "" || strings.HasPrefix(entry.Key, prefix) {
			keys = append(keys, entry.Key)
		}
	}

	sort.Strings(keys)
	return keys
}

// Collections returns the sorted list of collections currently holding live keys.
func (db *DB) Collections() []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	seen := map[string]struct{}{}
	for _, entry := range db.index {
		seen[entry.Collection] = struct{}{}
	}

	collections := make([]string, 0, len(seen))
	for collection := range seen {
		collections = append(collections, collection)
	}
	if len(collections) == 0 {
		collections = append(collections, DefaultCollection)
	}
	sort.Strings(collections)
	return collections
}

// CountRecordsInCollection returns the number of live keys in the requested collection after prefix filtering.
func (db *DB) CountRecordsInCollection(collection, prefix string) int {
	db.mu.RLock()
	defer db.mu.RUnlock()

	collection = normalizeCollection(collection)
	count := 0
	for _, entry := range db.index {
		if entry.Collection != collection {
			continue
		}
		if prefix == "" || strings.HasPrefix(entry.Key, prefix) {
			count++
		}
	}

	return count
}

// Records returns explorer rows with lazy value preview loading and optional pagination.
func (db *DB) Records(prefix string, offset int, limit int) []Record {
	return db.RecordsInCollection(DefaultCollection, prefix, offset, limit)
}

// RecordsInCollection returns paged explorer rows for one collection.
func (db *DB) RecordsInCollection(collection, prefix string, offset int, limit int) []Record {
	db.mu.RLock()
	keys := make([]string, 0, len(db.index))
	collection = normalizeCollection(collection)
	for _, entry := range db.index {
		if entry.Collection != collection {
			continue
		}
		if prefix == "" || strings.HasPrefix(entry.Key, prefix) {
			keys = append(keys, entry.Key)
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
		entries[key] = db.index[canonicalKey(collection, key)]
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
			Collection:   entry.Collection,
			Key:          key,
			ValuePreview: valuePreview(value),
			ValueSize:    entry.ValueSize,
			ValueKind:    entry.ValueKind,
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

func validateCollection(collection string) error {
	if strings.TrimSpace(collection) == "" {
		return fmt.Errorf("collection must not be empty")
	}
	if strings.Contains(collection, "/") {
		return fmt.Errorf("collection must not contain '/'")
	}
	return nil
}

func validateValueKind(valueKind string) error {
	if valueKind == "" {
		return nil
	}
	switch valueKind {
	case ValueKindRaw, ValueKindJSON:
		return nil
	default:
		return fmt.Errorf("unsupported value kind %q", valueKind)
	}
}

func normalizeCollection(collection string) string {
	if strings.TrimSpace(collection) == "" {
		return DefaultCollection
	}
	return strings.TrimSpace(collection)
}

func canonicalKey(collection, key string) string {
	return normalizeCollection(collection) + "/" + key
}

func splitCanonicalKey(composite string) (string, string) {
	firstSlash := strings.Index(composite, "/")
	if firstSlash == -1 {
		return DefaultCollection, composite
	}
	return normalizeCollection(composite[:firstSlash]), composite[firstSlash+1:]
}

func normalizeValueKind(valueKind string) string {
	if strings.TrimSpace(valueKind) == "" {
		return ValueKindRaw
	}
	return strings.TrimSpace(strings.ToLower(valueKind))
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
	case strings.HasPrefix(upper, "SETIN "):
		collection, key, value, err := parseCollectionSetCommand(commandText, "SETIN")
		if err != nil {
			return "", err
		}
		if err := db.SetInCollection(collection, key, value); err != nil {
			return "", err
		}
		return "OK", nil
	case strings.HasPrefix(upper, "SETJSON "):
		collection, key, value, err := parseCollectionSetCommand(commandText, "SETJSON")
		if err != nil {
			return "", err
		}
		if err := db.SetTypedInCollection(collection, key, value, ValueKindJSON); err != nil {
			return "", err
		}
		return "OK", nil
	case strings.HasPrefix(upper, "GETIN "):
		collection, key, err := parseCollectionKeyCommand(commandText, "GETIN")
		if err != nil {
			return "", err
		}
		value, ok := db.GetFromCollection(collection, key)
		if !ok {
			return "", ErrKeyNotFound
		}
		return value, nil
	case strings.HasPrefix(upper, "DELETEIN "):
		collection, key, err := parseCollectionKeyCommand(commandText, "DELETEIN")
		if err != nil {
			return "", err
		}
		if err := db.DeleteFromCollection(collection, key); err != nil {
			return "", err
		}
		return "OK", nil
	case upper == "KEYS":
		return strings.Join(db.Keys(""), "\n"), nil
	case strings.HasPrefix(upper, "KEYS "):
		prefix := strings.TrimSpace(commandText[5:])
		return strings.Join(db.Keys(prefix), "\n"), nil
	case upper == "COLLECTIONS":
		return strings.Join(db.Collections(), "\n"), nil
	case strings.HasPrefix(upper, "KEYSIN "):
		collection, prefix, err := parseCollectionPrefixCommand(commandText, "KEYSIN")
		if err != nil {
			return "", err
		}
		return strings.Join(db.KeysInCollection(collection, prefix), "\n"), nil
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
	case upper == "REPAIR":
		destinationDir := filepath.Join(db.dataDir, "repair-"+time.Now().UTC().Format("20060102-150405"))
		report, err := db.RepairTo(destinationDir)
		if err != nil {
			return "", err
		}
		return formatRepair(report), nil
	case upper == "EXPORT ALL":
		destinationPath := filepath.Join(db.dataDir, "export-all-"+time.Now().UTC().Format("20060102-150405")+".jsonl")
		report, err := db.ExportCollection("all", destinationPath)
		if err != nil {
			return "", err
		}
		return formatExport(report), nil
	case strings.HasPrefix(upper, "EXPORT "):
		collection := strings.TrimSpace(commandText[7:])
		destinationPath := filepath.Join(db.dataDir, "export-"+normalizeCollection(collection)+"-"+time.Now().UTC().Format("20060102-150405")+".jsonl")
		report, err := db.ExportCollection(collection, destinationPath)
		if err != nil {
			return "", err
		}
		return formatExport(report), nil
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

func parseCollectionSetCommand(commandText string, verb string) (string, string, string, error) {
	rest := strings.TrimSpace(commandText[len(verb):])
	firstSpace := strings.IndexAny(rest, " \t")
	if firstSpace == -1 {
		return "", "", "", fmt.Errorf("%s requires collection, key, and value", verb)
	}

	collection := strings.TrimSpace(rest[:firstSpace])
	remaining := strings.TrimLeft(rest[firstSpace+1:], " \t")
	secondSpace := strings.IndexAny(remaining, " \t")
	if secondSpace == -1 {
		return "", "", "", fmt.Errorf("%s requires collection, key, and value", verb)
	}

	key := strings.TrimSpace(remaining[:secondSpace])
	value := strings.TrimLeft(remaining[secondSpace+1:], " \t")
	if err := validateCollection(collection); err != nil {
		return "", "", "", err
	}
	if err := validateKey(key); err != nil {
		return "", "", "", err
	}

	return collection, key, value, nil
}

func parseCollectionKeyCommand(commandText string, verb string) (string, string, error) {
	rest := strings.TrimSpace(commandText[len(verb):])
	fields := strings.Fields(rest)
	if len(fields) != 2 {
		return "", "", fmt.Errorf("%s requires collection and key", verb)
	}
	if err := validateCollection(fields[0]); err != nil {
		return "", "", err
	}
	if err := validateKey(fields[1]); err != nil {
		return "", "", err
	}
	return fields[0], fields[1], nil
}

func parseCollectionPrefixCommand(commandText string, verb string) (string, string, error) {
	rest := strings.TrimSpace(commandText[len(verb):])
	if rest == "" {
		return "", "", fmt.Errorf("%s requires at least a collection", verb)
	}
	firstSpace := strings.IndexAny(rest, " \t")
	if firstSpace == -1 {
		if err := validateCollection(rest); err != nil {
			return "", "", err
		}
		return rest, "", nil
	}

	collection := strings.TrimSpace(rest[:firstSpace])
	prefix := strings.TrimSpace(rest[firstSpace+1:])
	if err := validateCollection(collection); err != nil {
		return "", "", err
	}
	return collection, prefix, nil
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
				Command:    commandSet,
				Collection: DefaultCollection,
				Key:        key,
				Value:      value,
				ValueKind:  ValueKindRaw,
			})
		case strings.HasPrefix(upper, "SETIN "):
			collection, key, value, err := parseCollectionSetCommand(line, "SETIN")
			if err != nil {
				return nil, err
			}
			operations = append(operations, BatchOperation{
				Command:    commandSet,
				Collection: collection,
				Key:        key,
				Value:      value,
				ValueKind:  ValueKindRaw,
			})
		case strings.HasPrefix(upper, "SETJSON "):
			collection, key, value, err := parseCollectionSetCommand(line, "SETJSON")
			if err != nil {
				return nil, err
			}
			operations = append(operations, BatchOperation{
				Command:    commandSet,
				Collection: collection,
				Key:        key,
				Value:      value,
				ValueKind:  ValueKindJSON,
			})
		case strings.HasPrefix(upper, "DELETE "):
			key := strings.TrimSpace(line[7:])
			if err := validateKey(key); err != nil {
				return nil, err
			}
			operations = append(operations, BatchOperation{
				Command:    commandDelete,
				Collection: DefaultCollection,
				Key:        key,
			})
		case strings.HasPrefix(upper, "DELETEIN "):
			collection, key, err := parseCollectionKeyCommand(line, "DELETEIN")
			if err != nil {
				return nil, err
			}
			operations = append(operations, BatchOperation{
				Command:    commandDelete,
				Collection: collection,
				Key:        key,
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

func formatExport(report ExportReport) string {
	return strings.Join([]string{
		"destination=" + report.DestinationPath,
		"collection=" + report.Collection,
		"exported_records=" + strconv.Itoa(report.ExportedRecords),
	}, "\n")
}

func formatRepair(report RepairReport) string {
	lines := []string{
		"destination=" + report.DestinationDir,
		"recovered_records=" + strconv.Itoa(report.RecoveredRecords),
		"used_snapshot=" + strconv.FormatBool(report.UsedSnapshot),
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
