package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type exportedRecord struct {
	Collection string    `json:"collection"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	ValueKind  string    `json:"value_kind"`
	ValueSize  int       `json:"value_size"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Sequence   uint64    `json:"sequence"`
}

// ExportCollection writes one collection or the whole database as newline-delimited JSON.
// Pass collection "all" or an empty string to export every live record.
func (db *DB) ExportCollection(collection, destinationPath string) (ExportReport, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	targetCollection := normalizeExportCollection(collection)
	keys := make([]string, 0, len(db.index))
	if targetCollection == "all" {
		for canonical := range db.index {
			keys = append(keys, canonical)
		}
		sort.Strings(keys)
	} else {
		collectionKeys := db.collectionKeys[targetCollection]
		for _, key := range collectionKeys {
			keys = append(keys, canonicalKey(targetCollection, key))
		}
	}

	return db.exportEntriesLocked(keys, destinationPath, targetCollection, "")
}

// ExportJSONQueryCollection writes only the live JSON records matching one query expression.
func (db *DB) ExportJSONQueryCollection(collection, queryText, destinationPath string) (ExportReport, error) {
	expression, err := ParseJSONQueryExpression(queryText)
	if err != nil {
		return ExportReport{}, err
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	targetCollection := normalizeCollection(collection)
	keys := db.findKeysByJSONExpressionLocked(targetCollection, "", expression)
	canonicalKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		canonicalKeys = append(canonicalKeys, canonicalKey(targetCollection, key))
	}

	return db.exportEntriesLocked(canonicalKeys, destinationPath, targetCollection, strings.TrimSpace(queryText))
}

func (db *DB) exportEntriesLocked(canonicalKeys []string, destinationPath, collection, queryText string) (ExportReport, error) {
	file, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return ExportReport{}, fmt.Errorf("create export file %q: %w", destinationPath, err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, canonical := range canonicalKeys {
		entry := db.index[canonical]
		value, err := db.readValueForEntry(entry)
		if err != nil {
			return ExportReport{}, fmt.Errorf("read value for export %q: %w", canonical, err)
		}

		if err := encoder.Encode(exportedRecord{
			Collection: entry.Collection,
			Key:        entry.Key,
			Value:      value,
			ValueKind:  entry.ValueKind,
			ValueSize:  entry.ValueSize,
			CreatedAt:  entry.CreatedAt,
			UpdatedAt:  entry.UpdatedAt,
			Sequence:   entry.LastSequence,
		}); err != nil {
			return ExportReport{}, fmt.Errorf("encode export record %q: %w", canonical, err)
		}
	}

	if err := file.Sync(); err != nil {
		return ExportReport{}, fmt.Errorf("sync export file: %w", err)
	}

	return ExportReport{
		DestinationPath: destinationPath,
		Collection:      collection,
		QueryText:       queryText,
		ExportedRecords: len(canonicalKeys),
	}, nil
}

func normalizeExportCollection(collection string) string {
	if strings.EqualFold(strings.TrimSpace(collection), "all") || strings.TrimSpace(collection) == "" {
		return "all"
	}
	return normalizeCollection(collection)
}
