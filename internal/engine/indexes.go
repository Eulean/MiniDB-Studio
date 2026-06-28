package engine

import (
	"sort"
)

// rebuildDerivedIndexesLocked recreates the sorted per-collection key slices from the canonical index map.
// We call this after bulk state replacement such as startup recovery, snapshot load, or compaction swap.
func (db *DB) rebuildDerivedIndexesLocked() {
	db.collectionKeys = make(map[string][]string)
	for _, entry := range db.index {
		db.collectionKeys[entry.Collection] = append(db.collectionKeys[entry.Collection], entry.Key)
	}

	for collection := range db.collectionKeys {
		sort.Strings(db.collectionKeys[collection])
	}

	db.rebuildJSONFieldIndexesLocked()
}

// upsertIndexEntryLocked updates the canonical entry map and keeps the collection's sorted key slice in sync.
func (db *DB) upsertIndexEntryLocked(entry indexEntry) {
	if previous, exists := db.index[entry.CanonicalKey]; exists {
		db.removeJSONFieldIndexLocked(previous)
	}

	db.index[entry.CanonicalKey] = entry

	keys := db.collectionKeys[entry.Collection]
	insertAt := sort.SearchStrings(keys, entry.Key)
	if insertAt < len(keys) && keys[insertAt] == entry.Key {
		db.addJSONFieldIndexLocked(entry)
		return
	}

	keys = append(keys, "")
	copy(keys[insertAt+1:], keys[insertAt:])
	keys[insertAt] = entry.Key
	db.collectionKeys[entry.Collection] = keys
	db.addJSONFieldIndexLocked(entry)
}

// deleteIndexEntryLocked removes a live record from both the canonical map and the sorted collection slice.
func (db *DB) deleteIndexEntryLocked(collection, key string) {
	canonical := canonicalKey(collection, key)
	if previous, exists := db.index[canonical]; exists {
		db.removeJSONFieldIndexLocked(previous)
	}
	delete(db.index, canonical)

	keys := db.collectionKeys[collection]
	removeAt := sort.SearchStrings(keys, key)
	if removeAt >= len(keys) || keys[removeAt] != key {
		return
	}

	keys = append(keys[:removeAt], keys[removeAt+1:]...)
	if len(keys) == 0 {
		delete(db.collectionKeys, collection)
		return
	}

	db.collectionKeys[collection] = keys
}

// collectionKeysRangeLocked returns the exact sorted slice window for one collection and optional prefix.
// The returned bounds let higher-level readers page without rescanning unrelated collections or keys.
func (db *DB) collectionKeysRangeLocked(collection, prefix string) ([]string, int, int) {
	keys := db.collectionKeys[collection]
	if prefix == "" {
		return keys, 0, len(keys)
	}

	start := sort.SearchStrings(keys, prefix)
	end := sort.SearchStrings(keys, prefix+"\xff")
	if start > end {
		start = end
	}

	return keys, start, end
}
