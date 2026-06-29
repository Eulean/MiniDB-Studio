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

var miniSQLPattern = regexp.MustCompile(`(?is)^\s*SELECT\s+(.+?)\s+FROM\s+([A-Za-z0-9._-]+)(?:\s+WHERE\s+(.+?))?(?:\s+GROUP\s+BY\s+([A-Za-z0-9._-]+))?(?:\s+ORDER\s+BY\s+([A-Za-z0-9._-]+)(?:\s+(ASC|DESC))?)?(?:\s+LIMIT\s+(\d+))?(?:\s+OFFSET\s+(\d+))?\s*$`)
var miniSQLCountDistinctPattern = regexp.MustCompile(`(?is)^COUNT\s*\(\s*DISTINCT\s+([A-Za-z0-9._-]+)\s*\)$`)

// SQLSelectQuery is the intentionally small read-only SQL surface for MiniDB v9.0.
type SQLSelectQuery struct {
	Columns         []string
	Collection      string
	WhereText       string
	GroupByColumn   string
	OrderByColumn   string
	OrderDesc       bool
	Limit           int
	Offset          int
	IsCountQuery    bool
	IsCountDistinct bool
	DistinctColumn  string
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

	records, err := runMiniSQLRecordQuery(a, query)
	if err != nil {
		return MiniSQLResult{}, err
	}
	fetchLimit := query.Limit
	if query.IsCountQuery || query.GroupByColumn != "" {
		fetchLimit = 0
	}
	_ = fetchLimit

	if query.GroupByColumn != "" {
		return runMiniSQLGroupBy(a, records, query)
	}

	if query.IsCountDistinct {
		distinctValues := make(map[string]struct{})
		for _, record := range records {
			distinctValues[miniSQLSortValue(a, record, query.DistinctColumn)] = struct{}{}
		}
		return MiniSQLResult{
			Headers:  []string{"count_distinct"},
			Rows:     [][]string{{strconv.Itoa(len(distinctValues))}},
			RowCount: 1,
		}, nil
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
		return SQLSelectQuery{}, fmt.Errorf("unsupported SQL syntax; use SELECT <columns> FROM <collection> [WHERE <json-query>] [GROUP BY column] [ORDER BY column ASC|DESC] [LIMIT n] [OFFSET n]")
	}

	columns, isCountDistinct, distinctColumn, err := parseMiniSQLColumns(matches[1])
	if err != nil {
		return SQLSelectQuery{}, err
	}

	limit := 100
	if matches[7] != "" {
		parsedLimit, err := strconv.Atoi(strings.TrimSpace(matches[7]))
		if err != nil || parsedLimit <= 0 {
			return SQLSelectQuery{}, fmt.Errorf("SQL LIMIT must be a positive integer")
		}
		limit = parsedLimit
	}
	offset := 0
	if matches[8] != "" {
		parsedOffset, err := strconv.Atoi(strings.TrimSpace(matches[8]))
		if err != nil || parsedOffset < 0 {
			return SQLSelectQuery{}, fmt.Errorf("SQL OFFSET must be zero or a positive integer")
		}
		offset = parsedOffset
	}

	query := SQLSelectQuery{
		Columns:         columns,
		Collection:      strings.TrimSpace(matches[2]),
		WhereText:       strings.TrimSpace(matches[3]),
		GroupByColumn:   strings.ToLower(strings.TrimSpace(matches[4])),
		OrderByColumn:   strings.ToLower(strings.TrimSpace(matches[5])),
		OrderDesc:       strings.EqualFold(strings.TrimSpace(matches[6]), "DESC"),
		Limit:           limit,
		Offset:          offset,
		IsCountQuery:    len(columns) == 1 && columns[0] == "count",
		IsCountDistinct: isCountDistinct,
		DistinctColumn:  distinctColumn,
	}

	if len(query.Columns) == 0 {
		return SQLSelectQuery{}, fmt.Errorf("SQL query must request at least one column")
	}
	if query.GroupByColumn != "" && !isSupportedMiniSQLGroupColumn(query.GroupByColumn) {
		return SQLSelectQuery{}, fmt.Errorf("unsupported SQL GROUP BY column %q", query.GroupByColumn)
	}
	if query.GroupByColumn != "" {
		if !isValidGroupBySelection(query.Columns, query.GroupByColumn) {
			return SQLSelectQuery{}, fmt.Errorf("GROUP BY queries currently require SELECT %s, COUNT(*)", query.GroupByColumn)
		}
	}
	if query.OrderByColumn != "" && !isSupportedMiniSQLOrderColumn(query.OrderByColumn, query.GroupByColumn != "") {
		return SQLSelectQuery{}, fmt.Errorf("unsupported SQL ORDER BY column %q", query.OrderByColumn)
	}
	if query.IsCountQuery && query.GroupByColumn != "" {
		return SQLSelectQuery{}, fmt.Errorf("use SELECT %s, COUNT(*) when combining COUNT(*) with GROUP BY", query.GroupByColumn)
	}
	if query.IsCountDistinct && query.GroupByColumn != "" {
		return SQLSelectQuery{}, fmt.Errorf("COUNT(DISTINCT ...) cannot currently be combined with GROUP BY")
	}

	return query, nil
}

func parseMiniSQLColumns(columnText string) ([]string, bool, string, error) {
	columnText = strings.TrimSpace(columnText)
	if columnText == "*" {
		return []string{"collection", "key", "value_preview", "value_kind", "value_size", "updated_at"}, false, "", nil
	}
	if strings.EqualFold(columnText, "COUNT(*)") {
		return []string{"count"}, false, "", nil
	}
	if matches := miniSQLCountDistinctPattern.FindStringSubmatch(columnText); len(matches) == 2 {
		column := strings.ToLower(strings.TrimSpace(matches[1]))
		if !isSupportedMiniSQLColumn(column) {
			return nil, false, "", fmt.Errorf("unsupported SQL DISTINCT column %q", matches[1])
		}
		return []string{"count_distinct"}, true, column, nil
	}

	parts := strings.Split(columnText, ",")
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		rawColumn := strings.TrimSpace(part)
		if strings.EqualFold(rawColumn, "COUNT(*)") {
			columns = append(columns, "count")
			continue
		}

		column := strings.ToLower(rawColumn)
		if column == "count" || isSupportedMiniSQLColumn(column) {
			columns = append(columns, column)
			continue
		}
		return nil, false, "", fmt.Errorf("unsupported SQL column %q", rawColumn)
	}

	return columns, false, "", nil
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

func isSupportedMiniSQLGroupColumn(column string) bool {
	switch column {
	case "collection", "value_kind":
		return true
	default:
		return false
	}
}

func isSupportedMiniSQLOrderColumn(column string, grouped bool) bool {
	if grouped && column == "count" {
		return true
	}
	return isSupportedMiniSQLColumn(column)
}

func isValidGroupBySelection(columns []string, groupByColumn string) bool {
	if len(columns) != 2 {
		return false
	}
	hasGroupColumn := false
	hasCount := false
	for _, column := range columns {
		if column == groupByColumn {
			hasGroupColumn = true
		}
		if column == "count" {
			hasCount = true
		}
	}
	return hasGroupColumn && hasCount
}

func runMiniSQLGroupBy(application *Application, records []engine.Record, query SQLSelectQuery) (MiniSQLResult, error) {
	counts := make(map[string]int)
	for _, record := range records {
		groupValue := miniSQLSortValue(application, record, query.GroupByColumn)
		counts[groupValue]++
	}

	rows := make([][]string, 0, len(counts))
	for groupValue, count := range counts {
		row := make([]string, 0, len(query.Columns))
		for _, column := range query.Columns {
			switch column {
			case query.GroupByColumn:
				row = append(row, groupValue)
			case "count":
				row = append(row, strconv.Itoa(count))
			default:
				return MiniSQLResult{}, fmt.Errorf("unsupported grouped SQL column %q", column)
			}
		}
		rows = append(rows, row)
	}

	if query.OrderByColumn != "" {
		sort.SliceStable(rows, func(i, j int) bool {
			leftText, leftNumber := miniSQLGroupedSortValue(query, rows[i])
			rightText, rightNumber := miniSQLGroupedSortValue(query, rows[j])
			if query.OrderByColumn == "count" {
				if query.OrderDesc {
					return leftNumber > rightNumber
				}
				return leftNumber < rightNumber
			}
			if query.OrderDesc {
				return leftText > rightText
			}
			return leftText < rightText
		})
	}
	if query.Limit > 0 && len(rows) > query.Limit {
		rows = rows[:query.Limit]
	}

	return MiniSQLResult{
		Headers:  query.Columns,
		Rows:     rows,
		RowCount: len(rows),
	}, nil
}

func runMiniSQLRecordQuery(application *Application, query SQLSelectQuery) ([]engine.Record, error) {
	fetchOffset := query.Offset
	fetchLimit := query.Limit
	if query.IsCountQuery || query.IsCountDistinct || query.GroupByColumn != "" {
		fetchOffset = 0
		fetchLimit = 0
	}

	if strings.TrimSpace(query.WhereText) == "" {
		return application.db.RecordsInCollection(query.Collection, "", fetchOffset, fetchLimit), nil
	}

	expression, err := engine.ParseJSONQueryExpression(query.WhereText)
	if err != nil {
		return nil, err
	}

	return application.db.RecordsByJSONExpressionInCollection(query.Collection, "", expression, fetchOffset, fetchLimit), nil
}

func miniSQLGroupedSortValue(query SQLSelectQuery, row []string) (string, int) {
	for index, column := range query.Columns {
		if column == query.OrderByColumn {
			if column == "count" {
				count, _ := strconv.Atoi(row[index])
				return row[index], count
			}
			return row[index], 0
		}
	}
	return "", 0
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
