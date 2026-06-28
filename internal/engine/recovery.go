package engine

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// recoverState loads the latest snapshot first, then replays segment records newer than the snapshot cutoff.
func (db *DB) recoverState() error {
	db.index = make(map[string]indexEntry)
	db.collectionKeys = make(map[string][]string)
	db.jsonFieldIndex = make(map[string]map[string]map[string][]string)
	reconstructCounters := db.metadata.TotalSetOperations == 0 &&
		db.metadata.TotalDeleteOps == 0 &&
		db.metadata.NextSequence == 1

	snapshotSequence, err := db.loadSnapshotLocked()
	if err != nil {
		return err
	}

	segmentPaths, err := db.segmentPaths()
	if err != nil {
		return err
	}

	var replayedRecords uint64
	for _, path := range segmentPaths {
		replayedFromSegment, err := db.replaySegment(path, snapshotSequence, reconstructCounters)
		if err != nil {
			return err
		}
		replayedRecords += replayedFromSegment
	}

	db.metadata.StartupReplayCount = replayedRecords
	return nil
}

func (db *DB) replaySegment(path string, snapshotSequence uint64, reconstructCounters bool) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open segment %q for replay: %w", path, err)
	}
	defer file.Close()

	reader, err := newFrameReader(file, true)
	if err != nil {
		return 0, err
	}

	var replayed uint64
	for {
		frame, err := reader.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("replay segment %q: %w", path, err)
		}

		batch, err := decodeMutationBatch(frame)
		if err != nil {
			return 0, fmt.Errorf("replay segment %q: %w", path, err)
		}

		for operationIndex, operation := range batch.Operations {
			if operation.Sequence <= snapshotSequence {
				continue
			}

			if operation.Sequence >= db.metadata.NextSequence {
				db.metadata.NextSequence = operation.Sequence + 1
			}

			switch operation.Command {
			case commandSet:
				collection := normalizeCollection(operation.Collection)
				key := operation.Key
				if operation.Collection == "" && strings.Contains(operation.Key, "/") {
					collection, key = splitCanonicalKey(operation.Key)
				}

				jsonFields := map[string][]string(nil)
				if normalizeValueKind(operation.ValueKind) == ValueKindJSON {
					jsonFields, err = extractIndexedJSONFields(operation.Value)
					if err != nil {
						return 0, fmt.Errorf("replay json field indexing for %s/%s: %w", collection, key, err)
					}
				}

				db.upsertIndexEntryLocked(indexEntry{
					CanonicalKey: canonicalKey(collection, key),
					Collection:   collection,
					Key:          key,
					ValueKind:    normalizeValueKind(operation.ValueKind),
					JSONFields:   jsonFields,
					SourceType:   sourceTypeSegment,
					SourcePath:   path,
					FrameOffset:  frame.Offset,
					OperationIdx: operationIndex,
					ValueSize:    operation.ValueSize,
					CreatedAt:    operation.CreatedAt,
					UpdatedAt:    operation.UpdatedAt,
					LastSequence: operation.Sequence,
				})
				if reconstructCounters {
					db.metadata.TotalSetOperations++
				}
			case commandDelete:
				collection := normalizeCollection(operation.Collection)
				key := operation.Key
				if operation.Collection == "" && strings.Contains(operation.Key, "/") {
					collection, key = splitCanonicalKey(operation.Key)
				}
				db.deleteIndexEntryLocked(collection, key)
				if reconstructCounters {
					db.metadata.TotalDeleteOps++
				}
			default:
				return 0, fmt.Errorf("log recovery error: unknown command %q", operation.Command)
			}

			replayed++
		}
	}

	return replayed, nil
}
