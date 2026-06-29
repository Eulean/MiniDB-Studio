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

	parts, err := tokenizeJSONQuery(queryText)
	if err != nil {
		return nil, err
	}

	conditions := make([]JSONQueryCondition, 0, len(parts))
	for _, part := range parts {
		if part == "(" || part == ")" || strings.EqualFold(part, "OR") || strings.EqualFold(part, "NOT") {
			return nil, fmt.Errorf("invalid JSON query condition %q: grouped expressions are not allowed here", part)
		}

		condition, err := parseJSONQueryCondition(part)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}

	return conditions, nil
}

// ParseJSONQueryExpression parses condition groups with implicit AND and explicit OR.
// Parentheses are compiled down into the same OR-of-ANDs structure used by the evaluator.
func ParseJSONQueryExpression(queryText string) (JSONQueryExpression, error) {
	queryText = strings.TrimSpace(queryText)
	if queryText == "" {
		return nil, nil
	}

	tokens, err := tokenizeJSONQuery(queryText)
	if err != nil {
		return nil, err
	}

	parser := jsonQueryParser{
		tokens: tokens,
	}
	expression, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if parser.hasNext() {
		return nil, fmt.Errorf("invalid JSON query expression near %q", parser.peek())
	}
	if len(expression) == 0 {
		return nil, fmt.Errorf("invalid JSON query expression: expected at least one condition")
	}

	return expression, nil
}

// tokenizeJSONQuery splits the query into condition, operator, and grouping tokens.
// Quotes keep values with spaces intact so the parser can support realistic string predicates.
func tokenizeJSONQuery(queryText string) ([]string, error) {
	tokens := make([]string, 0, len(queryText)/4)
	var current strings.Builder
	inQuotes := false
	escaping := false

	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range queryText {
		switch {
		case escaping:
			current.WriteRune(r)
			escaping = false
		case r == '\\' && inQuotes:
			current.WriteRune(r)
			escaping = true
		case r == '"':
			current.WriteRune(r)
			inQuotes = !inQuotes
		case inQuotes:
			current.WriteRune(r)
		case r == '(' || r == ')':
			flushCurrent()
			tokens = append(tokens, string(r))
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flushCurrent()
		default:
			current.WriteRune(r)
		}
	}

	if escaping {
		return nil, fmt.Errorf("invalid JSON query expression: unterminated escape sequence")
	}
	if inQuotes {
		return nil, fmt.Errorf("invalid JSON query expression: missing closing quote")
	}

	flushCurrent()
	if len(tokens) == 0 {
		return nil, nil
	}

	for _, part := range tokens {
		if part == "(" || part == ")" || strings.EqualFold(part, "OR") {
			continue
		}
		if strings.EqualFold(part, "NOT") {
			continue
		}

		if _, err := parseJSONQueryCondition(part); err != nil {
			return nil, err
		}
	}

	return tokens, nil
}

// jsonQueryParser compiles a grouped JSON query into disjunctive normal form.
// The evaluator stays simple because parser output is still an OR-of-ANDs slice structure.
type jsonQueryParser struct {
	tokens []string
	index  int
}

func (p *jsonQueryParser) parseExpression() (JSONQueryExpression, error) {
	left, err := p.parseOrExpression()
	if err != nil {
		return nil, err
	}
	return normalizeJSONQueryExpression(left), nil
}

func (p *jsonQueryParser) parseOrExpression() (JSONQueryExpression, error) {
	left, err := p.parseAndExpression()
	if err != nil {
		return nil, err
	}

	for p.hasNext() && strings.EqualFold(p.peek(), "OR") {
		p.next()
		right, err := p.parseAndExpression()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON query expression: OR must appear between condition groups")
		}
		left = append(left, right...)
	}

	return left, nil
}

func (p *jsonQueryParser) parseAndExpression() (JSONQueryExpression, error) {
	if !p.hasNext() || p.peek() == ")" || strings.EqualFold(p.peek(), "OR") {
		return nil, fmt.Errorf("invalid JSON query expression: expected a condition or group")
	}

	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}

	for p.hasNext() && p.peek() != ")" && !strings.EqualFold(p.peek(), "OR") {
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = andJSONQueryExpressions(left, right)
	}

	return left, nil
}

func (p *jsonQueryParser) parsePrimary() (JSONQueryExpression, error) {
	if !p.hasNext() {
		return nil, fmt.Errorf("invalid JSON query expression: expected a condition or group")
	}

	negateCount := 0
	for p.hasNext() && strings.EqualFold(p.peek(), "NOT") {
		negateCount++
		p.next()
	}
	if !p.hasNext() {
		return nil, fmt.Errorf("invalid JSON query expression: NOT must be followed by a condition or group")
	}

	token := p.next()
	if token == "(" {
		group, err := p.parseOrExpression()
		if err != nil {
			return nil, err
		}
		if !p.hasNext() || p.next() != ")" {
			return nil, fmt.Errorf("invalid JSON query expression: missing closing parenthesis")
		}
		if len(group) == 0 {
			return nil, fmt.Errorf("invalid JSON query expression: empty parentheses are not allowed")
		}
		if negateCount%2 == 1 {
			group = negateJSONQueryExpression(group)
		}
		return group, nil
	}
	if token == ")" {
		return nil, fmt.Errorf("invalid JSON query expression: unexpected closing parenthesis")
	}
	if strings.EqualFold(token, "OR") {
		return nil, fmt.Errorf("invalid JSON query expression: OR must appear between condition groups")
	}

	condition, err := parseJSONQueryCondition(token)
	if err != nil {
		return nil, err
	}
	if negateCount%2 == 1 {
		condition.Negated = !condition.Negated
	}

	return JSONQueryExpression{{condition}}, nil
}

func (p *jsonQueryParser) hasNext() bool {
	return p.index < len(p.tokens)
}

func (p *jsonQueryParser) peek() string {
	return p.tokens[p.index]
}

func (p *jsonQueryParser) next() string {
	token := p.tokens[p.index]
	p.index++
	return token
}

func andJSONQueryExpressions(left, right JSONQueryExpression) JSONQueryExpression {
	if len(left) == 0 {
		return normalizeJSONQueryExpression(right)
	}
	if len(right) == 0 {
		return normalizeJSONQueryExpression(left)
	}

	combined := make(JSONQueryExpression, 0, len(left)*len(right))
	for _, leftGroup := range left {
		for _, rightGroup := range right {
			merged := make([]JSONQueryCondition, 0, len(leftGroup)+len(rightGroup))
			merged = append(merged, leftGroup...)
			merged = append(merged, rightGroup...)
			combined = append(combined, merged)
		}
	}

	return normalizeJSONQueryExpression(combined)
}

// negateJSONQueryExpression applies De Morgan's law and keeps the output in OR-of-ANDs form.
// This lets NOT work on grouped expressions without adding a second evaluator shape.
func negateJSONQueryExpression(expression JSONQueryExpression) JSONQueryExpression {
	if len(expression) == 0 {
		return JSONQueryExpression{}
	}

	negated := JSONQueryExpression{{}}
	for _, group := range expression {
		if len(group) == 0 {
			continue
		}

		groupChoices := make(JSONQueryExpression, 0, len(group))
		for _, condition := range group {
			negatedCondition := condition
			negatedCondition.Negated = !negatedCondition.Negated
			groupChoices = append(groupChoices, []JSONQueryCondition{negatedCondition})
		}

		negated = andJSONQueryExpressions(negated, groupChoices)
	}

	return normalizeJSONQueryExpression(negated)
}

func normalizeJSONQueryExpression(expression JSONQueryExpression) JSONQueryExpression {
	normalized := make(JSONQueryExpression, 0, len(expression))
	for _, group := range expression {
		if len(group) == 0 {
			continue
		}

		normalizedGroup := make([]JSONQueryCondition, len(group))
		copy(normalizedGroup, group)
		normalized = append(normalized, normalizedGroup)
	}

	return normalized
}

func parseJSONQueryCondition(part string) (JSONQueryCondition, error) {
	path, operator, value, err := splitJSONQueryCondition(part)
	if err != nil {
		return JSONQueryCondition{}, err
	}
	if path == "" {
		return JSONQueryCondition{}, fmt.Errorf("invalid JSON query condition %q: path must not be empty", part)
	}
	if value == "" {
		return JSONQueryCondition{}, fmt.Errorf("invalid JSON query condition %q: value must not be empty", part)
	}
	if strings.HasPrefix(value, "\"") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return JSONQueryCondition{}, fmt.Errorf("invalid JSON query condition %q: %w", part, err)
		}
		value = unquoted
	}

	return JSONQueryCondition{
		Negated:  false,
		Path:     path,
		Operator: operator,
		Value:    value,
	}, nil
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
	matched := false

	switch condition.Operator {
	case "", "=":
		for _, value := range values {
			if value == condition.Value {
				matched = true
				break
			}
		}
	case "~=":
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), strings.ToLower(condition.Value)) {
				matched = true
				break
			}
		}
	case ">", ">=", "<", "<=":
		queryNumber, err := strconv.ParseFloat(condition.Value, 64)
		if err != nil {
			return condition.Negated
		}
		for _, value := range values {
			valueNumber, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			switch condition.Operator {
			case ">":
				if valueNumber > queryNumber {
					matched = true
				}
			case ">=":
				if valueNumber >= queryNumber {
					matched = true
				}
			case "<":
				if valueNumber < queryNumber {
					matched = true
				}
			case "<=":
				if valueNumber <= queryNumber {
					matched = true
				}
			}
			if matched {
				break
			}
		}
	}

	if condition.Negated {
		return !matched
	}

	return matched
}
