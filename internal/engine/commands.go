package engine

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Set stores or replaces a value for a key and durably appends the change to disk.
func (db *DB) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	record := logRecord{
		Command:   commandSet,
		Key:       key,
		Value:     value,
		Timestamp: time.Now().UTC(),
	}

	if err := appendRecord(db.logFile, record); err != nil {
		return fmt.Errorf("set %q: %w", key, err)
	}

	db.index[key] = value
	db.setOps++
	if err := db.saveMetadata(); err != nil {
		return fmt.Errorf("persist metadata after set %q: %w", key, err)
	}
	return nil
}

// Get returns a value by key and reports whether the key exists.
func (db *DB) Get(key string) (string, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	value, ok := db.index[key]
	return value, ok
}

// Delete removes a key when it exists and records the tombstone durably.
func (db *DB) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.index[key]; !exists {
		return ErrKeyNotFound
	}

	record := logRecord{
		Command:   commandDelete,
		Key:       key,
		Timestamp: time.Now().UTC(),
	}

	if err := appendRecord(db.logFile, record); err != nil {
		return fmt.Errorf("delete %q: %w", key, err)
	}

	delete(db.index, key)
	db.deleteOps++
	if err := db.saveMetadata(); err != nil {
		return fmt.Errorf("persist metadata after delete %q: %w", key, err)
	}
	return nil
}

// Keys lists keys in sorted order and optionally filters by a prefix.
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

// Records returns live records sorted by key for the explorer page.
func (db *DB) Records(prefix string) []Record {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keys := make([]string, 0, len(db.index))
	for key := range db.index {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	records := make([]Record, 0, len(keys))
	for _, key := range keys {
		records = append(records, Record{
			Key:   key,
			Value: db.index[key],
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

// Execute parses and runs one MiniDB v1 command for the console page.
func (db *DB) Execute(input string) (string, error) {
	commandLine := strings.TrimSpace(input)
	if commandLine == "" {
		return "", fmt.Errorf("command must not be empty")
	}

	upper := strings.ToUpper(commandLine)

	switch {
	case strings.HasPrefix(upper, "SET "):
		key, value, err := parseSetCommand(commandLine)
		if err != nil {
			return "", err
		}
		if err := db.Set(key, value); err != nil {
			return "", err
		}
		return "OK", nil
	case strings.HasPrefix(upper, "GET "):
		key := strings.TrimSpace(commandLine[4:])
		value, ok := db.Get(key)
		if !ok {
			return "", ErrKeyNotFound
		}
		return value, nil
	case strings.HasPrefix(upper, "DELETE "):
		key := strings.TrimSpace(commandLine[7:])
		if err := db.Delete(key); err != nil {
			return "", err
		}
		return "OK", nil
	case upper == "KEYS":
		return strings.Join(db.Keys(""), "\n"), nil
	case strings.HasPrefix(upper, "KEYS "):
		prefix := strings.TrimSpace(commandLine[5:])
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
	default:
		return "", fmt.Errorf("unsupported command: %s", commandLine)
	}
}

// parseSetCommand preserves everything after the key as the value, including embedded newlines.
func parseSetCommand(commandLine string) (string, string, error) {
	rest := strings.TrimSpace(commandLine[4:])
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

func formatStats(stats Stats) string {
	return strings.Join([]string{
		"live_key_count=" + strconv.Itoa(stats.LiveKeyCount),
		"log_file_size=" + strconv.FormatInt(stats.LogFileSize, 10),
		"total_set_operations=" + strconv.FormatUint(stats.TotalSetOperations, 10),
		"total_delete_operations=" + strconv.FormatUint(stats.TotalDeleteOps, 10),
		"last_compaction_time=" + stats.LastCompactionTime.Format(time.RFC3339),
	}, "\n")
}
