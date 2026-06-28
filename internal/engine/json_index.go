package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// extractIndexedJSONFields flattens indexed JSON scalars into dot-path keys such as profile.email.
// Arrays of scalars are also indexed in v3.6 so membership queries can reuse the same path=value syntax.
func extractIndexedJSONFields(value string) (map[string][]string, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode json for indexing: %w", err)
	}

	object, ok := payload.(map[string]any)
	if !ok {
		return map[string][]string{}, nil
	}

	fields := make(map[string][]string)
	for field, rawValue := range object {
		flattenIndexedJSONFields(field, rawValue, fields)
	}

	return fields, nil
}

func flattenIndexedJSONFields(path string, rawValue any, fields map[string][]string) {
	switch typed := rawValue.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			flattenIndexedJSONFields(path+"."+childKey, childValue, fields)
		}
	case []any:
		for _, item := range typed {
			appendIndexedScalar(path, item, fields)
		}
	default:
		appendIndexedScalar(path, typed, fields)
	}
}

func appendIndexedScalar(path string, rawValue any, fields map[string][]string) {
	switch typed := rawValue.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			flattenIndexedJSONFields(path+"."+childKey, childValue, fields)
		}
	case []any:
		for _, item := range typed {
			appendIndexedScalar(path, item, fields)
		}
	case string:
		fields[path] = appendUniqueScalar(fields[path], typed)
	case json.Number:
		fields[path] = appendUniqueScalar(fields[path], typed.String())
	case bool:
		fields[path] = appendUniqueScalar(fields[path], strconv.FormatBool(typed))
	case nil:
		fields[path] = appendUniqueScalar(fields[path], "null")
	case float64:
		if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
			fields[path] = appendUniqueScalar(fields[path], strconv.FormatFloat(typed, 'f', -1, 64))
		}
	}
}

func appendUniqueScalar(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// ParseJSONQueryConditions parses a space-delimited list of path=value predicates.
// Values are intentionally simple in v3.6 and must not contain unescaped spaces.
func ParseJSONQueryConditions(queryText string) ([]JSONQueryCondition, error) {
	queryText = strings.TrimSpace(queryText)
	if queryText == "" {
		return nil, nil
	}

	parts := strings.Fields(queryText)
	conditions := make([]JSONQueryCondition, 0, len(parts))
	for _, part := range parts {
		path, operator, value, err := splitJSONQueryCondition(part)
		if err != nil {
			return nil, err
		}
		if path == "" {
			return nil, fmt.Errorf("invalid JSON query condition %q: path must not be empty", part)
		}
		if value == "" {
			return nil, fmt.Errorf("invalid JSON query condition %q: value must not be empty", part)
		}

		conditions = append(conditions, JSONQueryCondition{
			Path:     path,
			Operator: operator,
			Value:    value,
		})
	}

	return conditions, nil
}

// ParseJSONQueryExpression parses OR-separated condition groups.
// Conditions inside one group remain ANDed together.
func ParseJSONQueryExpression(queryText string) (JSONQueryExpression, error) {
	queryText = strings.TrimSpace(queryText)
	if queryText == "" {
		return nil, nil
	}

	parts := strings.Fields(queryText)
	groups := make(JSONQueryExpression, 0, 1)
	currentGroup := make([]string, 0, len(parts))

	flushGroup := func() error {
		if len(currentGroup) == 0 {
			return fmt.Errorf("invalid JSON query expression: OR must appear between condition groups")
		}

		conditions, err := ParseJSONQueryConditions(strings.Join(currentGroup, " "))
		if err != nil {
			return err
		}
		groups = append(groups, conditions)
		currentGroup = currentGroup[:0]
		return nil
	}

	for _, part := range parts {
		if strings.EqualFold(part, "OR") {
			if err := flushGroup(); err != nil {
				return nil, err
			}
			continue
		}
		currentGroup = append(currentGroup, part)
	}

	if err := flushGroup(); err != nil {
		return nil, err
	}

	return groups, nil
}

func splitJSONQueryCondition(part string) (string, string, string, error) {
	operators := []string{">=", "<=", "~=", ">", "<", "="}
	for _, operator := range operators {
		if index := strings.Index(part, operator); index != -1 {
			path := strings.TrimSpace(part[:index])
			value := part[index+len(operator):]
			return path, operator, value, nil
		}
	}
	return "", "", "", fmt.Errorf("invalid JSON query condition %q: expected path=value, path~=value, or numeric comparison", part)
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

	for field, values := range entry.JSONFields {
		valueIndex := collectionIndex[field]
		if valueIndex == nil {
			valueIndex = make(map[string][]string)
			collectionIndex[field] = valueIndex
		}

		for _, value := range values {
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

	for field, values := range entry.JSONFields {
		valueIndex := collectionIndex[field]
		if valueIndex == nil {
			continue
		}

		for _, value := range values {
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
		}

		if len(valueIndex) == 0 {
			delete(collectionIndex, field)
		}
	}

	if len(collectionIndex) == 0 {
		delete(db.jsonFieldIndex, entry.Collection)
	}
}

func evaluateJSONCondition(values []string, condition JSONQueryCondition) bool {
	switch condition.Operator {
	case "", "=":
		for _, value := range values {
			if value == condition.Value {
				return true
			}
		}
	case "~=":
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), strings.ToLower(condition.Value)) {
				return true
			}
		}
	case ">", ">=", "<", "<=":
		queryNumber, err := strconv.ParseFloat(condition.Value, 64)
		if err != nil {
			return false
		}
		for _, value := range values {
			valueNumber, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			switch condition.Operator {
			case ">":
				if valueNumber > queryNumber {
					return true
				}
			case ">=":
				if valueNumber >= queryNumber {
					return true
				}
			case "<":
				if valueNumber < queryNumber {
					return true
				}
			case "<=":
				if valueNumber <= queryNumber {
					return true
				}
			}
		}
	}

	return false
}
