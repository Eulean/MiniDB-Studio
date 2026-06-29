package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"minidb-studio/internal/engine"
)

// SchemaFieldSummary describes one observed JSON field path inside a collection.
type SchemaFieldSummary struct {
	Path               string
	ObservedDocuments  int
	DistinctValueCount int
	SampleValues       []string
}

// CollectionSchemaSummary groups observed field information for one collection.
type CollectionSchemaSummary struct {
	Collection      string
	JSONRecordCount int
	Fields          []SchemaFieldSummary
}

type schemaAccumulator struct {
	observedCount int
	values        map[string]struct{}
	samples       []string
}

// InspectCollectionSchema summarizes scalar JSON fields for one collection.
func (a *Application) InspectCollectionSchema(collection string) (CollectionSchemaSummary, error) {
	collection = strings.TrimSpace(collection)
	if collection == "" {
		return CollectionSchemaSummary{}, fmt.Errorf("collection must not be empty")
	}

	keys := a.db.KeysInCollection(collection, "")
	accumulators := make(map[string]*schemaAccumulator)
	jsonRecordCount := 0

	for _, key := range keys {
		metadata, ok := a.db.GetRecordMetadataInCollection(collection, key)
		if !ok || metadata.ValueKind != engine.ValueKindJSON {
			continue
		}

		value, ok := a.db.GetFromCollection(collection, key)
		if !ok {
			continue
		}

		var payload any
		if err := json.Unmarshal([]byte(value), &payload); err != nil {
			continue
		}

		jsonRecordCount++
		collectSchemaFields(payload, "", accumulators)
	}

	fields := make([]SchemaFieldSummary, 0, len(accumulators))
	for path, accumulator := range accumulators {
		fields = append(fields, SchemaFieldSummary{
			Path:               path,
			ObservedDocuments:  accumulator.observedCount,
			DistinctValueCount: len(accumulator.values),
			SampleValues:       append([]string(nil), accumulator.samples...),
		})
	}

	sort.Slice(fields, func(i, j int) bool {
		if fields[i].ObservedDocuments == fields[j].ObservedDocuments {
			return fields[i].Path < fields[j].Path
		}
		return fields[i].ObservedDocuments > fields[j].ObservedDocuments
	})

	return CollectionSchemaSummary{
		Collection:      collection,
		JSONRecordCount: jsonRecordCount,
		Fields:          fields,
	}, nil
}

func collectSchemaFields(payload any, prefix string, accumulators map[string]*schemaAccumulator) {
	switch typed := payload.(type) {
	case map[string]any:
		for key, value := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			collectSchemaFields(value, path, accumulators)
		}
	case []any:
		for _, value := range typed {
			collectSchemaFields(value, prefix, accumulators)
		}
	case string, float64, bool, nil:
		if prefix == "" {
			return
		}

		accumulator := accumulators[prefix]
		if accumulator == nil {
			accumulator = &schemaAccumulator{
				values: make(map[string]struct{}),
			}
			accumulators[prefix] = accumulator
		}

		accumulator.observedCount++
		normalized := normalizeSchemaValue(typed)
		accumulator.values[normalized] = struct{}{}
		if len(accumulator.samples) < 3 && !containsString(accumulator.samples, normalized) {
			accumulator.samples = append(accumulator.samples, normalized)
		}
	}
}

func normalizeSchemaValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
