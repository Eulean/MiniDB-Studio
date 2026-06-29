package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const importSampleKeyLimit = 8
const importProblemLimit = 8
const importFieldSummaryLimit = 20
const importChangeSampleLimit = 8
const importDiffFieldLimit = 10

type parsedImportDocument struct {
	Key           string
	CanonicalJSON string
}

type importFieldAccumulator struct {
	ObservedCount int
	Types         map[string]struct{}
}

// PreviewNDJSONImport analyzes one NDJSON file without writing any records.
func (db *DB) PreviewNDJSONImport(collection, sourcePath, keyField, conflictMode string) (ImportPreviewReport, error) {
	preview, _, err := db.analyzeNDJSONImport(collection, sourcePath, keyField, conflictMode)
	if err != nil {
		return ImportPreviewReport{}, err
	}
	return preview, nil
}

// ImportNDJSON loads newline-delimited JSON documents into one collection.
// Each line must be one JSON object containing the requested key field.
func (db *DB) ImportNDJSON(collection, sourcePath, keyField, conflictMode string, dryRun bool) (ImportReport, error) {
	preview, documents, err := db.analyzeNDJSONImport(collection, sourcePath, keyField, conflictMode)
	if err != nil {
		return ImportReport{}, err
	}
	if preview.InvalidLines > 0 || preview.MissingKeyCount > 0 {
		return ImportReport{}, fmt.Errorf("import blocked: preview found invalid_lines=%d missing_keys=%d", preview.InvalidLines, preview.MissingKeyCount)
	}

	report := ImportReport{
		SourcePath:      sourcePath,
		Collection:      preview.Collection,
		KeyField:        preview.KeyField,
		ConflictMode:    preview.ConflictMode,
		DryRun:          dryRun,
		SkippedRecords:  0,
		ImportedRecords: 0,
	}

	if dryRun {
		for _, document := range documents {
			if preview.ConflictMode == ImportConflictSkip {
				if _, exists := db.GetRecordMetadataInCollection(preview.Collection, document.Key); exists {
					report.SkippedRecords++
					continue
				}
			}
			report.ImportedRecords++
		}
		return report, nil
	}

	for _, document := range documents {
		if preview.ConflictMode == ImportConflictSkip {
			if _, exists := db.GetRecordMetadataInCollection(preview.Collection, document.Key); exists {
				report.SkippedRecords++
				continue
			}
		}

		if err := db.SetTypedInCollection(preview.Collection, document.Key, document.CanonicalJSON, ValueKindJSON); err != nil {
			return ImportReport{}, fmt.Errorf("import key %q: %w", document.Key, err)
		}
		report.ImportedRecords++
	}

	return report, nil
}

// analyzeNDJSONImport scans a source file once and returns both a preview report and parsed documents.
// Parsed documents are returned only for valid key-bearing records so dry-run and import can reuse the same work.
func (db *DB) analyzeNDJSONImport(collection, sourcePath, keyField, conflictMode string) (ImportPreviewReport, []parsedImportDocument, error) {
	collection = normalizeCollection(collection)
	if err := validateCollection(collection); err != nil {
		return ImportPreviewReport{}, nil, err
	}

	keyField = normalizeImportKeyField(keyField)
	conflictMode, err := normalizeImportConflictMode(conflictMode)
	if err != nil {
		return ImportPreviewReport{}, nil, err
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return ImportPreviewReport{}, nil, fmt.Errorf("open import file %q: %w", sourcePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	preview := ImportPreviewReport{
		SourcePath:    sourcePath,
		Collection:    collection,
		KeyField:      keyField,
		ConflictMode:  conflictMode,
		SampleKeys:    make([]string, 0, importSampleKeyLimit),
		FirstProblems: make([]string, 0, importProblemLimit),
		ChangeSamples: make([]ImportPreviewChangeSample, 0, importChangeSampleLimit),
	}
	documents := make([]parsedImportDocument, 0)
	seenKeys := make(map[string]int)
	conflictCounted := make(map[string]struct{})
	fieldAccumulators := make(map[string]*importFieldAccumulator)

	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		preview.TotalLines++
		key, canonicalJSON, payload, err := parseImportLine(line, keyField)
		if err != nil {
			errText := err.Error()
			switch {
			case strings.Contains(errText, "missing key field"):
				preview.MissingKeyCount++
			default:
				preview.InvalidLines++
			}
			appendImportProblem(&preview, fmt.Sprintf("line %d: %s", lineNumber, errText))
			continue
		}

		if seenKeys[key] > 0 {
			preview.DuplicateKeysInFile++
			appendImportProblem(&preview, fmt.Sprintf("line %d: duplicate key %q in source file", lineNumber, key))
		}
		seenKeys[key]++

		if len(preview.SampleKeys) < importSampleKeyLimit && !containsString(preview.SampleKeys, key) {
			preview.SampleKeys = append(preview.SampleKeys, key)
		}
		collectImportFieldSummaries(payload, "", fieldAccumulators)

		existingValue, exists := db.GetFromCollection(collection, key)
		if exists {
			if _, counted := conflictCounted[key]; !counted {
				preview.ExistingKeyConflicts++
				conflictCounted[key] = struct{}{}
			}
		}

		switch {
		case !exists:
			preview.NewRecordCount++
			appendImportChangeSample(&preview, ImportPreviewChangeSample{
				Key:             key,
				Status:          "new",
				IncomingPreview: valuePreview(canonicalJSON),
			})
		case conflictMode == ImportConflictSkip:
			preview.SkipCount++
			appendImportChangeSample(&preview, ImportPreviewChangeSample{
				Key:             key,
				Status:          "skip",
				CurrentPreview:  valuePreview(existingValue),
				IncomingPreview: valuePreview(canonicalJSON),
			})
		default:
			preview.OverwriteCount++
			changeSample := ImportPreviewChangeSample{
				Key:             key,
				Status:          "overwrite",
				CurrentPreview:  valuePreview(existingValue),
				IncomingPreview: valuePreview(canonicalJSON),
			}
			populateImportDiffSummary(&changeSample, existingValue, canonicalJSON)
			appendImportChangeSample(&preview, changeSample)
		}

		preview.ValidDocuments++
		documents = append(documents, parsedImportDocument{
			Key:           key,
			CanonicalJSON: canonicalJSON,
		})
	}

	if err := scanner.Err(); err != nil {
		return ImportPreviewReport{}, nil, fmt.Errorf("scan import file %q: %w", sourcePath, err)
	}

	preview.FieldSummaries = buildImportFieldSummaries(fieldAccumulators)
	preview.TotalDistinctFields = len(preview.FieldSummaries)

	return preview, documents, nil
}

func appendImportProblem(report *ImportPreviewReport, problem string) {
	if len(report.FirstProblems) < importProblemLimit {
		report.FirstProblems = append(report.FirstProblems, problem)
	}
}

func appendImportChangeSample(report *ImportPreviewReport, sample ImportPreviewChangeSample) {
	if len(report.ChangeSamples) < importChangeSampleLimit {
		report.ChangeSamples = append(report.ChangeSamples, sample)
	}
}

func populateImportDiffSummary(sample *ImportPreviewChangeSample, currentJSON, incomingJSON string) {
	currentFields, currentOK := flattenImportDiffJSON(currentJSON)
	incomingFields, incomingOK := flattenImportDiffJSON(incomingJSON)
	if !currentOK || !incomingOK {
		sample.ChangedFields = []string{"$document"}
		return
	}

	added := make([]string, 0)
	removed := make([]string, 0)
	changed := make([]string, 0)

	for path, incomingValue := range incomingFields {
		currentValue, exists := currentFields[path]
		switch {
		case !exists:
			added = append(added, path)
		case currentValue != incomingValue:
			changed = append(changed, path)
		}
	}
	for path := range currentFields {
		if _, exists := incomingFields[path]; !exists {
			removed = append(removed, path)
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	sample.AddedFields = limitImportDiffFields(added)
	sample.RemovedFields = limitImportDiffFields(removed)
	sample.ChangedFields = limitImportDiffFields(changed)
}

func flattenImportDiffJSON(raw string) (map[string]string, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}

	fields := make(map[string]string)
	flattenImportDiffValue("", payload, fields)
	return fields, true
}

func flattenImportDiffValue(path string, value any, fields map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		if path != "" {
			fields[path] = "{object}"
		}
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			flattenImportDiffValue(childPath, child, fields)
		}
	case []any:
		fields[path] = "{array}"
		for index, item := range typed {
			itemPath := fmt.Sprintf("%s[%d]", path, index)
			if path == "" {
				itemPath = fmt.Sprintf("[%d]", index)
			}
			flattenImportDiffValue(itemPath, item, fields)
		}
	case string:
		fields[path] = "string:" + typed
	case json.Number:
		fields[path] = "number:" + typed.String()
	case bool:
		fields[path] = "bool:" + strconv.FormatBool(typed)
	case nil:
		fields[path] = "null"
	case float64:
		fields[path] = "number:" + strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		fields[path] = fmt.Sprintf("%T", typed)
	}
}

func limitImportDiffFields(paths []string) []string {
	if len(paths) <= importDiffFieldLimit {
		return paths
	}
	return paths[:importDiffFieldLimit]
}

// parseImportLine validates one NDJSON document and extracts its stable record key.
// The document is re-marshaled into compact canonical JSON before being stored.
func parseImportLine(line, keyField string) (string, string, any, error) {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return "", "", nil, fmt.Errorf("invalid json object: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", "", nil, fmt.Errorf("invalid json object: extra data after document")
	}

	object, ok := payload.(map[string]any)
	if !ok {
		return "", "", nil, fmt.Errorf("expected top-level json object")
	}

	rawKey, ok := object[keyField]
	if !ok {
		return "", "", nil, fmt.Errorf("missing key field %q", keyField)
	}

	key, err := stringifyImportKey(rawKey)
	if err != nil {
		return "", "", nil, fmt.Errorf("invalid key field %q: %w", keyField, err)
	}
	if err := validateKey(key); err != nil {
		return "", "", nil, err
	}

	canonicalJSON, err := marshalCanonicalJSON(payload)
	if err != nil {
		return "", "", nil, fmt.Errorf("marshal canonical json: %w", err)
	}

	return key, canonicalJSON, payload, nil
}

func stringifyImportKey(raw any) (string, error) {
	switch typed := raw.(type) {
	case string:
		return typed, nil
	case json.Number:
		return typed.String(), nil
	case bool:
		return strconv.FormatBool(typed), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("key must be a string, number, or boolean scalar")
	}
}

func marshalCanonicalJSON(payload any) (string, error) {
	buffer := bytes.Buffer{}
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return "", err
	}

	return strings.TrimSpace(buffer.String()), nil
}

func normalizeImportKeyField(keyField string) string {
	keyField = strings.TrimSpace(keyField)
	if keyField == "" {
		return "id"
	}
	return keyField
}

func normalizeImportConflictMode(conflictMode string) (string, error) {
	conflictMode = strings.ToLower(strings.TrimSpace(conflictMode))
	if conflictMode == "" {
		return ImportConflictSkip, nil
	}

	switch conflictMode {
	case ImportConflictSkip, ImportConflictOverwrite:
		return conflictMode, nil
	default:
		return "", fmt.Errorf("unsupported import conflict mode %q", conflictMode)
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

func collectImportFieldSummaries(payload any, prefix string, accumulators map[string]*importFieldAccumulator) {
	switch typed := payload.(type) {
	case map[string]any:
		for key, value := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			collectNestedImportValue(path, value, accumulators)
		}
	}
}

func collectNestedImportValue(path string, value any, accumulators map[string]*importFieldAccumulator) {
	switch typed := value.(type) {
	case map[string]any:
		recordImportField(path, "object", accumulators)
		for childKey, childValue := range typed {
			childPath := path + "." + childKey
			collectNestedImportValue(childPath, childValue, accumulators)
		}
	case []any:
		recordImportField(path, "array", accumulators)
		for _, item := range typed {
			collectImportArrayItem(path, item, accumulators)
		}
	default:
		recordImportField(path, importJSONType(value), accumulators)
	}
}

func collectImportArrayItem(path string, value any, accumulators map[string]*importFieldAccumulator) {
	switch typed := value.(type) {
	case map[string]any:
		recordImportField(path+"[]", "object", accumulators)
		for childKey, childValue := range typed {
			childPath := path + "[]." + childKey
			collectNestedImportValue(childPath, childValue, accumulators)
		}
	case []any:
		recordImportField(path+"[]", "array", accumulators)
		for _, item := range typed {
			collectImportArrayItem(path+"[]", item, accumulators)
		}
	default:
		recordImportField(path+"[]", importJSONType(value), accumulators)
	}
}

func recordImportField(path, valueType string, accumulators map[string]*importFieldAccumulator) {
	accumulator := accumulators[path]
	if accumulator == nil {
		accumulator = &importFieldAccumulator{
			Types: make(map[string]struct{}),
		}
		accumulators[path] = accumulator
	}
	accumulator.ObservedCount++
	accumulator.Types[valueType] = struct{}{}
}

func importJSONType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "unknown"
	}
}

func buildImportFieldSummaries(accumulators map[string]*importFieldAccumulator) []ImportFieldSummary {
	paths := make([]string, 0, len(accumulators))
	for path := range accumulators {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	summaries := make([]ImportFieldSummary, 0, len(paths))
	for _, path := range paths {
		accumulator := accumulators[path]
		types := make([]string, 0, len(accumulator.Types))
		for valueType := range accumulator.Types {
			types = append(types, valueType)
		}
		sort.Strings(types)

		summaries = append(summaries, ImportFieldSummary{
			Path:          path,
			ObservedCount: accumulator.ObservedCount,
			Types:         types,
		})
	}

	if len(summaries) > importFieldSummaryLimit {
		return summaries[:importFieldSummaryLimit]
	}
	return summaries
}
