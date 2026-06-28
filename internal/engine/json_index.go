package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// extractIndexedJSONFields returns top-level scalar fields that MiniDB can query without SQL.
// Nested objects and arrays are intentionally skipped in v3.4 to keep the index small and predictable.
func extractIndexedJSONFields(value string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode json for indexing: %w", err)
	}

	object, ok := payload.(map[string]any)
	if !ok {
		return map[string]string{}, nil
	}

	fields := make(map[string]string)
	for field, rawValue := range object {
		switch typed := rawValue.(type) {
		case string:
			fields[field] = typed
		case json.Number:
			fields[field] = typed.String()
		case bool:
			fields[field] = strconv.FormatBool(typed)
		case nil:
			fields[field] = "null"
		}
	}

	return fields, nil
}

// rebuildJSONFieldIndexesLocked recreates the JSON equality index from the live canonical entries.
func (db *DB) rebuildJSONFieldIndexesLocked() {
	db.jsonFieldIndex = make(map[string]map[string]map[string][]string)
	for _, entry := range db.index {
		db.addJSONFieldIndexLocked(entry)
	}
}

// addJSONFieldIndexLocked inserts one live JSON record into the equality index.
func (db *DB) addJSONFieldIndexLocked(entry indexEntry) {
	if len(entry.JSONFields) == 0 {
		return
	}

	collectionIndex := db.jsonFieldIndex[entry.Collection]
	if collectionIndex == nil {
		collectionIndex = make(map[string]map[string][]string)
		db.jsonFieldIndex[entry.Collection] = collectionIndex
	}

	for field, value := range entry.JSONFields {
		valueIndex := collectionIndex[field]
		if valueIndex == nil {
			valueIndex = make(map[string][]string)
			collectionIndex[field] = valueIndex
		}

		keys := valueIndex[value]
		insertAt := sort.SearchStrings(keys, entry.Key)
		if insertAt < len(keys) && keys[insertAt] == entry.Key {
			continue
		}

		keys = append(keys, "")
		copy(keys[insertAt+1:], keys[insertAt:])
		keys[insertAt] = entry.Key
		valueIndex[value] = keys
	}
}

// removeJSONFieldIndexLocked removes one live JSON record from every indexed field bucket it occupied.
func (db *DB) removeJSONFieldIndexLocked(entry indexEntry) {
	if len(entry.JSONFields) == 0 {
		return
	}

	collectionIndex := db.jsonFieldIndex[entry.Collection]
	if collectionIndex == nil {
		return
	}

	for field, value := range entry.JSONFields {
		valueIndex := collectionIndex[field]
		if valueIndex == nil {
			continue
		}

		keys := valueIndex[value]
		removeAt := sort.SearchStrings(keys, entry.Key)
		if removeAt >= len(keys) || keys[removeAt] != entry.Key {
			continue
		}

		keys = append(keys[:removeAt], keys[removeAt+1:]...)
		if len(keys) == 0 {
			delete(valueIndex, value)
		} else {
			valueIndex[value] = keys
		}

		if len(valueIndex) == 0 {
			delete(collectionIndex, field)
		}
	}

	if len(collectionIndex) == 0 {
		delete(db.jsonFieldIndex, entry.Collection)
	}
}
