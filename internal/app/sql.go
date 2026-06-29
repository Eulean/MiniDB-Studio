package app

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"minidb-studio/internal/engine"
)

var miniSQLPattern = regexp.MustCompile(`(?is)^\s*SELECT\s+(.+?)\s+FROM\s+([A-Za-z0-9._-]+)(?:\s+WHERE\s+(.+?))?(?:\s+ORDER\s+BY\s+([A-Za-z0-9._-]+)(?:\s+(ASC|DESC))?)?(?:\s+LIMIT\s+(\d+))?\s*$`)

// SQLSelectQuery is the intentionally small read-only SQL surface for MiniDB v9.0.
type SQLSelectQuery struct {
	Columns       []string
	Collection    string
	WhereText     string
	OrderByColumn string
	OrderDesc     bool
	Limit         int
	IsCountQuery  bool
}

// MiniSQLResult is the structured query output shared by text rendering and file export.
type MiniSQLResult struct {
	Headers  []string
	Rows     [][]string
	RowCount int
}

// ExecuteMiniSQL translates one read-only SQL-style query into the existing MiniDB query engine.
func (a *Application) ExecuteMiniSQL(input string) (string, error) {
	result, err := a.RunMiniSQL(input)
	if err != nil {
		return "", err
	}

	lines := []string{
		strings.Join(result.Headers, "\t"),
	}
	for _, row := range result.Rows {
		lines = append(lines, strings.Join(row, "\t"))
	}
	lines = append(lines, fmt.Sprintf("%d row(s)", result.RowCount))
	return strings.Join(lines, "\n"), nil
}

// RunMiniSQL executes one read-only SQL-style query and returns a structured result set.
func (a *Application) RunMiniSQL(input string) (MiniSQLResult, error) {
	query, err := parseMiniSQLSelect(input)
	if err != nil {
		return MiniSQLResult{}, err
	}

	var records []engine.Record
	if strings.TrimSpace(query.WhereText) == "" {
		records, _ = a.ListRecords(query.Collection, "", 0, query.Limit)
	} else {
		records, _, err = a.ListRecordsByJSONQuery(query.Collection, "", query.WhereText, 0, query.Limit)
		if err != nil {
			return MiniSQLResult{}, err
		}
	}

	if query.OrderByColumn != "" {
		sortMiniSQLRecords(a, records, query.OrderByColumn, query.OrderDesc)
	}

	if query.Limit > 0 && len(records) > query.Limit {
		records = records[:query.Limit]
	}

	if query.IsCountQuery {
		return MiniSQLResult{
			Headers:  []string{"count"},
			Rows:     [][]string{{strconv.Itoa(len(records))}},
			RowCount: 1,
		}, nil
	}

	rows := make([][]string, 0, len(records))
	for _, record := range records {
		row, err := formatMiniSQLValues(a, record, query.Columns)
		if err != nil {
			return MiniSQLResult{}, err
		}
		rows = append(rows, row)
	}

	return MiniSQLResult{
		Headers:  query.Columns,
		Rows:     rows,
		RowCount: len(rows),
	}, nil
}

// ExportMiniSQLResult writes one SQL result set to a TSV file for offline analysis.
func (a *Application) ExportMiniSQLResult(input, destinationPath string) error {
	a.state.SetCurrentStatus("Exporting query result...")
	result, err := a.RunMiniSQL(input)
	if err != nil {
		a.state.SetCurrentStatus("Query export failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Export query result", strings.TrimSpace(destinationPath), err)
		return err
	}

	if strings.TrimSpace(destinationPath) == "" {
		return fmt.Errorf("destination path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create query export directory: %w", err)
	}

	file, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create query export file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if _, err := writer.WriteString(strings.Join(result.Headers, "\t") + "\n"); err != nil {
		return fmt.Errorf("write query export header: %w", err)
	}
	for _, row := range result.Rows {
		if _, err := writer.WriteString(strings.Join(row, "\t") + "\n"); err != nil {
			return fmt.Errorf("write query export row: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush query export file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync query export file: %w", err)
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Exported query result to %q", destinationPath))
	a.recordSuccessActivity("Export query result", strings.TrimSpace(destinationPath), fmt.Sprintf("Exported %d row(s)", result.RowCount))
	return nil
}

func parseMiniSQLSelect(input string) (SQLSelectQuery, error) {
	matches := miniSQLPattern.FindStringSubmatch(strings.TrimSpace(input))
	if len(matches) == 0 {
		return SQLSelectQuery{}, fmt.Errorf("unsupported SQL syntax; use SELECT <columns> FROM <collection> [WHERE <json-query>] [ORDER BY column ASC|DESC] [LIMIT n]")
	}

	columns, err := parseMiniSQLColumns(matches[1])
	if err != nil {
		return SQLSelectQuery{}, err
	}

	limit := 100
	if matches[6] != "" {
		parsedLimit, err := strconv.Atoi(strings.TrimSpace(matches[6]))
		if err != nil || parsedLimit <= 0 {
			return SQLSelectQuery{}, fmt.Errorf("SQL LIMIT must be a positive integer")
		}
		limit = parsedLimit
	}

	query := SQLSelectQuery{
		Columns:       columns,
		Collection:    strings.TrimSpace(matches[2]),
		WhereText:     strings.TrimSpace(matches[3]),
		OrderByColumn: strings.ToLower(strings.TrimSpace(matches[4])),
		OrderDesc:     strings.EqualFold(strings.TrimSpace(matches[5]), "DESC"),
		Limit:         limit,
		IsCountQuery:  len(columns) == 1 && columns[0] == "count",
	}

	if len(query.Columns) == 0 {
		return SQLSelectQuery{}, fmt.Errorf("SQL query must request at least one column")
	}
	if query.OrderByColumn != "" && !isSupportedMiniSQLColumn(query.OrderByColumn) {
		return SQLSelectQuery{}, fmt.Errorf("unsupported SQL ORDER BY column %q", query.OrderByColumn)
	}

	return query, nil
}

func parseMiniSQLColumns(columnText string) ([]string, error) {
	columnText = strings.TrimSpace(columnText)
	if columnText == "*" {
		return []string{"collection", "key", "value_preview", "value_kind", "value_size", "updated_at"}, nil
	}
	if strings.EqualFold(columnText, "COUNT(*)") {
		return []string{"count"}, nil
	}

	parts := strings.Split(columnText, ",")
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		column := strings.ToLower(strings.TrimSpace(part))
		if isSupportedMiniSQLColumn(column) {
			columns = append(columns, column)
			continue
		}
		return nil, fmt.Errorf("unsupported SQL column %q", strings.TrimSpace(part))
	}

	return columns, nil
}

func formatMiniSQLValues(application *Application, record engine.Record, columns []string) ([]string, error) {
	values := make([]string, 0, len(columns))
	for _, column := range columns {
		switch column {
		case "collection":
			values = append(values, record.Collection)
		case "key":
			values = append(values, record.Key)
		case "value":
			fullValue, ok := application.GetRecord(record.Collection, record.Key)
			if !ok {
				values = append(values, "")
				continue
			}
			values = append(values, sanitizeMiniSQLCell(fullValue))
		case "value_preview":
			values = append(values, sanitizeMiniSQLCell(record.ValuePreview))
		case "value_kind":
			values = append(values, record.ValueKind)
		case "value_size":
			values = append(values, strconv.Itoa(record.ValueSize))
		case "created_at":
			values = append(values, formatMiniSQLTime(record.CreatedAt))
		case "updated_at":
			values = append(values, formatMiniSQLTime(record.UpdatedAt))
		default:
			return nil, fmt.Errorf("unsupported SQL column %q", column)
		}
	}

	return values, nil
}

func isSupportedMiniSQLColumn(column string) bool {
	switch column {
	case "collection", "key", "value", "value_preview", "value_kind", "value_size", "created_at", "updated_at":
		return true
	default:
		return false
	}
}

func sortMiniSQLRecords(application *Application, records []engine.Record, column string, desc bool) {
	sort.SliceStable(records, func(i, j int) bool {
		left, right := miniSQLSortValue(application, records[i], column), miniSQLSortValue(application, records[j], column)
		if desc {
			return left > right
		}
		return left < right
	})
}

func miniSQLSortValue(application *Application, record engine.Record, column string) string {
	switch column {
	case "collection":
		return record.Collection
	case "key":
		return record.Key
	case "value":
		value, _ := application.GetRecord(record.Collection, record.Key)
		return sanitizeMiniSQLCell(value)
	case "value_preview":
		return sanitizeMiniSQLCell(record.ValuePreview)
	case "value_kind":
		return record.ValueKind
	case "value_size":
		return fmt.Sprintf("%012d", record.ValueSize)
	case "created_at":
		return record.CreatedAt.UTC().Format(time.RFC3339Nano)
	case "updated_at":
		return record.UpdatedAt.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

func sanitizeMiniSQLCell(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ")
	return replacer.Replace(value)
}

func formatMiniSQLTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
