package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"minidb-studio/internal/engine"
)

// Application coordinates the database engine and the desktop UI state.
type Application struct {
	db            *engine.DB
	state         *State
	presetStore   *DatasetPresetStore
	queryStore    *QueryStore
	activityStore *ActivityStore
}

// NewApplication opens MiniDB and prepares the shared application state.
func NewApplication() (*Application, error) {
	db, err := engine.Open()
	if err != nil {
		return nil, err
	}

	return &Application{
		db:            db,
		state:         NewState(db.DataDir()),
		presetStore:   NewDatasetPresetStore(db.DataDir()),
		queryStore:    NewQueryStore(db.DataDir()),
		activityStore: NewActivityStore(db.DataDir()),
	}, nil
}

// NewApplicationForTests creates an application wrapper around an existing DB for tests.
func NewApplicationForTests(db *engine.DB, presetStore *DatasetPresetStore) *Application {
	if presetStore == nil {
		presetStore = NewDatasetPresetStore(db.DataDir())
	}

	return &Application{
		db:            db,
		state:         NewState(db.DataDir()),
		presetStore:   presetStore,
		queryStore:    NewQueryStore(db.DataDir()),
		activityStore: NewActivityStore(db.DataDir()),
	}
}

// Close releases database resources when the desktop window exits.
func (a *Application) Close() error {
	return a.db.Close()
}

// State exposes the shared UI state to window components.
func (a *Application) State() *State {
	return a.state
}

// ListDatasetPresets exposes reusable workflow presets for the maintenance UI.
func (a *Application) ListDatasetPresets() ([]DatasetPreset, error) {
	return a.presetStore.List()
}

// ListSavedQueries returns the reusable SQL query library for the query studio page.
func (a *Application) ListSavedQueries() ([]SavedQuery, error) {
	return a.queryStore.List()
}

// FindSavedQuery resolves one reusable SQL query by name.
func (a *Application) FindSavedQuery(name string) (SavedQuery, error) {
	return a.queryStore.Find(name)
}

// SaveSavedQuery persists one reusable SQL query.
func (a *Application) SaveSavedQuery(query SavedQuery) error {
	if err := a.queryStore.Save(query); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Save query", strings.TrimSpace(query.Name), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Saved query %q", strings.TrimSpace(query.Name)))
	a.recordSuccessActivity("Save query", strings.TrimSpace(query.Name), "Saved read-only SQL query")
	return nil
}

// DeleteSavedQuery removes one saved SQL query by name.
func (a *Application) DeleteSavedQuery(name string) error {
	if err := a.queryStore.Delete(name); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Delete query", strings.TrimSpace(name), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Deleted query %q", strings.TrimSpace(name)))
	a.recordSuccessActivity("Delete query", strings.TrimSpace(name), "Deleted saved SQL query")
	return nil
}

// RenameSavedQuery changes one saved SQL query name.
func (a *Application) RenameSavedQuery(oldName, newName string) error {
	if err := a.queryStore.Rename(oldName, newName); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Rename query", strings.TrimSpace(oldName), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Renamed query %q to %q", strings.TrimSpace(oldName), strings.TrimSpace(newName)))
	a.recordSuccessActivity("Rename query", strings.TrimSpace(newName), "Renamed saved SQL query")
	return nil
}

// FindDatasetPreset resolves one reusable workflow preset by name.
func (a *Application) FindDatasetPreset(name string) (DatasetPreset, error) {
	return a.presetStore.Find(name)
}

// SaveDatasetPreset persists one reusable workflow preset.
func (a *Application) SaveDatasetPreset(preset DatasetPreset) error {
	if err := a.presetStore.Save(preset); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Save preset", strings.TrimSpace(preset.Name), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Saved preset %q", strings.TrimSpace(preset.Name)))
	a.recordSuccessActivity("Save preset", strings.TrimSpace(preset.Name), "Saved or updated preset configuration")
	return nil
}

// DeleteDatasetPreset removes one reusable workflow preset by name.
func (a *Application) DeleteDatasetPreset(name string) error {
	if err := a.presetStore.Delete(name); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Delete preset", strings.TrimSpace(name), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Deleted preset %q", strings.TrimSpace(name)))
	a.recordSuccessActivity("Delete preset", strings.TrimSpace(name), "Deleted saved preset")
	return nil
}

// RenameDatasetPreset changes one preset name while preserving the saved workflow fields.
func (a *Application) RenameDatasetPreset(oldName, newName string) error {
	if err := a.presetStore.Rename(oldName, newName); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Rename preset", strings.TrimSpace(oldName), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Renamed preset %q to %q", strings.TrimSpace(oldName), strings.TrimSpace(newName)))
	a.recordSuccessActivity("Rename preset", strings.TrimSpace(newName), "Renamed saved preset")
	return nil
}

// DuplicateDatasetPreset copies one preset to a new name while preserving its workflow fields.
func (a *Application) DuplicateDatasetPreset(sourceName, newName string) error {
	if err := a.presetStore.Duplicate(sourceName, newName); err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Duplicate preset", strings.TrimSpace(sourceName), err)
		return err
	}
	a.state.SetLastResult(fmt.Sprintf("Duplicated preset %q to %q", strings.TrimSpace(sourceName), strings.TrimSpace(newName)))
	a.recordSuccessActivity("Duplicate preset", strings.TrimSpace(newName), "Created a copied preset")
	return nil
}

// ExportDatasetPresetConfig writes one saved preset to a standalone JSON file.
func (a *Application) ExportDatasetPresetConfig(name, path string) (DatasetPreset, error) {
	preset, err := a.presetStore.Export(name, path)
	if err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Export preset config", strings.TrimSpace(name), err)
		return DatasetPreset{}, err
	}
	a.state.SetLastResult(fmt.Sprintf("Exported preset %q to %q", strings.TrimSpace(name), strings.TrimSpace(path)))
	a.recordSuccessActivity("Export preset config", strings.TrimSpace(name), fmt.Sprintf("Exported preset to %q", strings.TrimSpace(path)))
	return preset, nil
}

// ImportDatasetPresetConfig loads one standalone preset JSON file into local preset storage.
func (a *Application) ImportDatasetPresetConfig(path string) (DatasetPreset, error) {
	preset, err := a.presetStore.Import(path)
	if err != nil {
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Import preset config", strings.TrimSpace(path), err)
		return DatasetPreset{}, err
	}
	a.state.SetLastResult(fmt.Sprintf("Imported preset %q from %q", preset.Name, strings.TrimSpace(path)))
	a.recordSuccessActivity("Import preset config", preset.Name, fmt.Sprintf("Imported preset from %q", strings.TrimSpace(path)))
	return preset, nil
}

// DataDir exposes the active storage path for the status bar and maintenance page.
func (a *Application) DataDir() string {
	return a.db.DataDir()
}

// ListCollections returns the current live collections for the explorer and console.
func (a *Application) ListCollections() []string {
	return a.db.Collections()
}

// ListRecords returns a paged set of live records for the explorer page.
func (a *Application) ListRecords(collection, prefix string, page, pageSize int) ([]engine.Record, int) {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	offset := page * pageSize
	total := a.db.CountRecordsInCollection(collection, prefix)
	return a.db.RecordsInCollection(collection, prefix, offset, pageSize), total
}

// ListRecordsByJSONField returns paged records matching one JSON field equality filter.
func (a *Application) ListRecordsByJSONField(collection, prefix, field, value string, page, pageSize int) ([]engine.Record, int) {
	records, total, _ := a.ListRecordsByJSONQuery(collection, prefix, field+"="+value, page, pageSize)
	return records, total
}

// ListRecordsByJSONQuery returns paged records matching a parsed JSON query expression.
func (a *Application) ListRecordsByJSONQuery(collection, prefix, queryText string, page, pageSize int) ([]engine.Record, int, error) {
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	expression, err := engine.ParseJSONQueryExpression(queryText)
	if err != nil {
		return nil, 0, err
	}

	offset := page * pageSize
	total := a.db.CountRecordsByJSONExpressionInCollection(collection, prefix, expression)
	return a.db.RecordsByJSONExpressionInCollection(collection, prefix, expression, offset, pageSize), total, nil
}

// GetRecord returns one record value for details or editing.
func (a *Application) GetRecord(collection, key string) (string, bool) {
	return a.db.GetFromCollection(collection, key)
}

// GetRecordMetadata returns the metadata shown in the explorer details panel.
func (a *Application) GetRecordMetadata(collection, key string) (engine.EntryMetadata, bool) {
	return a.db.GetRecordMetadataInCollection(collection, key)
}

// SaveRecord creates or updates a key-value pair and updates status text.
func (a *Application) SaveRecord(collection, key, value, valueKind string) error {
	a.state.SetCurrentStatus("Saving record...")

	if err := a.db.SetTypedInCollection(collection, key, value, valueKind); err != nil {
		a.state.SetCurrentStatus("Save failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Save record", collection+"/"+key, err)
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Saved record %q/%q", collection, key))
	a.recordSuccessActivity("Save record", collection+"/"+key, fmt.Sprintf("Stored %s record", valueKind))
	return nil
}

// RenameRecord handles a key rename by creating the new key and removing the old one.
func (a *Application) RenameRecord(oldCollection, oldKey, newCollection, newKey, value, valueKind string) error {
	a.state.SetCurrentStatus("Renaming record...")

	if oldCollection == newCollection && oldKey == newKey {
		return a.SaveRecord(newCollection, newKey, value, valueKind)
	}

	if err := a.db.ApplyBatch([]engine.BatchOperation{
		{Command: "SET", Collection: newCollection, Key: newKey, Value: value, ValueKind: valueKind},
		{Command: "DELETE", Collection: oldCollection, Key: oldKey},
	}); err != nil {
		a.state.SetCurrentStatus("Rename failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Rename record", oldCollection+"/"+oldKey, err)
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Renamed record %q/%q to %q/%q", oldCollection, oldKey, newCollection, newKey))
	a.recordSuccessActivity("Rename record", newCollection+"/"+newKey, fmt.Sprintf("Moved record from %s/%s", oldCollection, oldKey))
	return nil
}

// DeleteRecord removes a key from the database and updates status text.
func (a *Application) DeleteRecord(collection, key string) error {
	a.state.SetCurrentStatus("Deleting record...")

	if err := a.db.DeleteFromCollection(collection, key); err != nil {
		a.state.SetCurrentStatus("Delete failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Delete record", collection+"/"+key, err)
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Deleted record %q/%q", collection, key))
	a.recordSuccessActivity("Delete record", collection+"/"+key, "Deleted live record")
	return nil
}

// ExecuteCommand runs one MiniDB v1 console command and stores history output.
func (a *Application) ExecuteCommand(input string) (string, error) {
	a.state.SetCurrentStatus("Running command...")

	result, err := a.executePresetAwareCommand(input)
	historyEntry := formatHistoryEntry(input, result, err)
	a.state.AppendHistory(historyEntry)

	if err != nil {
		a.state.SetCurrentStatus("Command failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Console command", strings.TrimSpace(input), err)
		return "", err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Command completed")
	a.recordSuccessActivity("Console command", strings.TrimSpace(input), "Command completed successfully")
	return result, nil
}

func (a *Application) executePresetAwareCommand(input string) (string, error) {
	commandText := strings.TrimSpace(input)
	upper := strings.ToUpper(commandText)

	switch {
	case strings.HasPrefix(upper, "SELECT "):
		return a.ExecuteMiniSQL(commandText)
	case upper == "LISTPRESETS":
		presets, err := a.ListDatasetPresets()
		if err != nil {
			return "", err
		}
		return formatPresetListResult(presets), nil
	case strings.HasPrefix(upper, "SAVEPRESET "):
		preset, err := parseSavePresetCommand(commandText)
		if err != nil {
			return "", err
		}
		if err := a.SaveDatasetPreset(preset); err != nil {
			return "", err
		}
		return formatSavedPresetResult(preset), nil
	case strings.HasPrefix(upper, "DELETEPRESET "):
		presetName, err := parseDeletePresetCommand(commandText)
		if err != nil {
			return "", err
		}
		if err := a.DeleteDatasetPreset(presetName); err != nil {
			return "", err
		}
		return formatDeletedPresetResult(presetName), nil
	case strings.HasPrefix(upper, "RENAMEDPRESET "):
		oldName, newName, err := parseRenamePresetCommand(commandText)
		if err != nil {
			return "", err
		}
		if err := a.RenameDatasetPreset(oldName, newName); err != nil {
			return "", err
		}
		return formatRenamedPresetResult(oldName, newName), nil
	case strings.HasPrefix(upper, "DUPLICATEPRESET "):
		sourceName, newName, err := parseDuplicatePresetCommand(commandText)
		if err != nil {
			return "", err
		}
		if err := a.DuplicateDatasetPreset(sourceName, newName); err != nil {
			return "", err
		}
		return formatDuplicatedPresetResult(sourceName, newName), nil
	case strings.HasPrefix(upper, "EXPORTPRESETCONFIG "):
		presetName, destinationPath, err := parsePresetPathCommand(commandText, "EXPORTPRESETCONFIG")
		if err != nil {
			return "", err
		}
		preset, err := a.ExportDatasetPresetConfig(presetName, destinationPath)
		if err != nil {
			return "", err
		}
		return formatExportedPresetConfigResult(preset, destinationPath), nil
	case strings.HasPrefix(upper, "IMPORTPRESETCONFIG "):
		sourcePath, err := parsePresetConfigImportCommand(commandText)
		if err != nil {
			return "", err
		}
		preset, err := a.ImportDatasetPresetConfig(sourcePath)
		if err != nil {
			return "", err
		}
		return formatImportedPresetConfigResult(preset, sourcePath), nil
	case strings.HasPrefix(upper, "SHOWPRESET "):
		presetName, err := parseShowPresetCommand(commandText)
		if err != nil {
			return "", err
		}
		preset, err := a.FindDatasetPreset(presetName)
		if err != nil {
			return "", err
		}
		return formatShowPresetResult(preset), nil
	case strings.HasPrefix(upper, "PREVIEWPRESET "):
		presetName, sourcePath, err := parsePresetPathCommand(commandText, "PREVIEWPRESET")
		if err != nil {
			return "", err
		}
		preset, err := a.FindDatasetPreset(presetName)
		if err != nil {
			return "", err
		}
		report, err := a.db.PreviewNDJSONImport(preset.Collection, sourcePath, preset.KeyField, preset.ConflictMode)
		if err != nil {
			return "", err
		}
		return formatPresetPreviewResult(preset, report), nil
	case strings.HasPrefix(upper, "IMPORTPRESET "):
		presetName, sourcePath, dryRun, err := parseImportPresetCommand(commandText)
		if err != nil {
			return "", err
		}
		preset, err := a.FindDatasetPreset(presetName)
		if err != nil {
			return "", err
		}
		report, err := a.db.ImportNDJSON(preset.Collection, sourcePath, preset.KeyField, preset.ConflictMode, dryRun)
		if err != nil {
			return "", err
		}
		return formatPresetImportResult(preset, report), nil
	case strings.HasPrefix(upper, "EXPORTPRESET "):
		presetName, destinationPath, err := parsePresetPathCommand(commandText, "EXPORTPRESET")
		if err != nil {
			return "", err
		}
		preset, err := a.FindDatasetPreset(presetName)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(preset.QueryText) == "" {
			return "", fmt.Errorf("preset %q does not define a query for EXPORTPRESET", preset.Name)
		}
		report, err := a.db.ExportJSONQueryCollection(preset.Collection, preset.QueryText, destinationPath)
		if err != nil {
			return "", err
		}
		return formatPresetExportResult(preset, report), nil
	default:
		return a.db.Execute(input)
	}
}

// CommandHistory returns the console history text for the output panel.
func (a *Application) CommandHistory() string {
	return a.state.Snapshot().CommandHistory
}

// Stats returns database statistics for the maintenance page.
func (a *Application) Stats() (engine.Stats, error) {
	return a.db.Stats()
}

// Snapshot creates a crash-safe snapshot of the current live database state.
func (a *Application) Snapshot() error {
	a.state.SetCurrentStatus("Creating snapshot...")

	if err := a.db.Snapshot(); err != nil {
		a.state.SetCurrentStatus("Snapshot failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Create snapshot", "database", err)
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Snapshot created")
	a.recordSuccessActivity("Create snapshot", "database", "Created durable snapshot")
	return nil
}

// Validate inspects the snapshot and segment files and returns the validation report.
func (a *Application) Validate() (engine.ValidationReport, error) {
	a.state.SetCurrentStatus("Validating database...")

	report, err := a.db.Validate()
	if err != nil {
		a.state.SetCurrentStatus("Validation failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Validate database", "database", err)
		return engine.ValidationReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Validation completed")
	a.recordSuccessActivity("Validate database", "database", fmt.Sprintf("Validated %d segment(s)", report.SegmentCount))
	return report, nil
}

// MaintenanceReport exposes recommendation heuristics for the maintenance page.
func (a *Application) MaintenanceReport() (engine.MaintenanceReport, error) {
	return a.db.MaintenanceReport()
}

// Compact runs durable log compaction and updates status text.
func (a *Application) Compact() error {
	a.state.SetCurrentStatus("Compacting database...")

	if err := a.db.Compact(); err != nil {
		a.state.SetCurrentStatus("Compaction failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Compact database", "database", err)
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Database compaction completed")
	a.recordSuccessActivity("Compact database", "database", "Compaction completed")
	return nil
}

// BackupToPath writes a copy of the current durable log to the selected path.
func (a *Application) BackupToPath(path string) error {
	a.state.SetCurrentStatus("Creating backup...")

	if err := a.db.BackupTo(path); err != nil {
		a.state.SetCurrentStatus("Backup failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Create backup", strings.TrimSpace(path), err)
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Backup created at %q", path))
	a.recordSuccessActivity("Create backup", strings.TrimSpace(path), "Created durable backup archive")
	return nil
}

// ExportCollection writes one collection or the full database to a chosen destination file.
func (a *Application) ExportCollection(collection, path string) (engine.ExportReport, error) {
	a.state.SetCurrentStatus("Exporting data...")

	report, err := a.db.ExportCollection(collection, path)
	if err != nil {
		a.state.SetCurrentStatus("Export failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Export collection", normalizeActivityTarget(collection, path), err)
		return engine.ExportReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Export created at %q", path))
	a.recordSuccessActivity("Export collection", normalizeActivityTarget(collection, path), fmt.Sprintf("Exported %d record(s)", report.ExportedRecords))
	return report, nil
}

// ExportJSONQueryCollection writes the matching JSON document slice to the requested destination.
func (a *Application) ExportJSONQueryCollection(collection, queryText, path string) (engine.ExportReport, error) {
	a.state.SetCurrentStatus("Exporting filtered data...")

	report, err := a.db.ExportJSONQueryCollection(collection, queryText, path)
	if err != nil {
		a.state.SetCurrentStatus("Filtered export failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Export filtered collection", normalizeActivityTarget(collection, path), err)
		return engine.ExportReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Filtered export created at %q", path))
	a.recordSuccessActivity("Export filtered collection", normalizeActivityTarget(collection, path), fmt.Sprintf("Exported %d filtered record(s)", report.ExportedRecords))
	return report, nil
}

// PreviewNDJSONImport analyzes one NDJSON file without writing records.
func (a *Application) PreviewNDJSONImport(collection, path, keyField, conflictMode string) (engine.ImportPreviewReport, error) {
	a.state.SetCurrentStatus("Previewing NDJSON import...")

	report, err := a.db.PreviewNDJSONImport(collection, path, keyField, conflictMode)
	if err != nil {
		a.state.SetCurrentStatus("Preview failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Preview import", normalizeActivityTarget(collection, path), err)
		return engine.ImportPreviewReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Previewed %d NDJSON lines from %q", report.TotalLines, path))
	a.recordSuccessActivity("Preview import", normalizeActivityTarget(collection, path), fmt.Sprintf("Previewed %d line(s)", report.TotalLines))
	return report, nil
}

// ImportNDJSON loads one NDJSON file into the requested collection and updates status text.
func (a *Application) ImportNDJSON(collection, path, keyField, conflictMode string, dryRun bool) (engine.ImportReport, error) {
	a.state.SetCurrentStatus("Importing NDJSON data...")

	report, err := a.db.ImportNDJSON(collection, path, keyField, conflictMode, dryRun)
	if err != nil {
		a.state.SetCurrentStatus("Import failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Import NDJSON", normalizeActivityTarget(collection, path), err)
		return engine.ImportReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	if dryRun {
		a.state.SetLastResult(fmt.Sprintf("Dry-run import checked %d records from %q", report.ImportedRecords+report.SkippedRecords, path))
		a.recordSuccessActivity("Import dry-run", normalizeActivityTarget(collection, path), fmt.Sprintf("Checked %d record(s)", report.ImportedRecords+report.SkippedRecords))
	} else {
		a.state.SetLastResult(fmt.Sprintf("Imported %d records from %q", report.ImportedRecords, path))
		a.recordSuccessActivity("Import NDJSON", normalizeActivityTarget(collection, path), fmt.Sprintf("Imported %d record(s)", report.ImportedRecords))
	}
	return report, nil
}

// DefaultRepairDir returns a sensible destination folder for salvage output.
func (a *Application) DefaultRepairDir() string {
	return filepath.Join(a.db.DataDir(), "repair-output")
}

// RepairTo salvages valid records into a fresh database directory.
func (a *Application) RepairTo(path string) (engine.RepairReport, error) {
	a.state.SetCurrentStatus("Repairing database...")

	report, err := a.db.RepairTo(path)
	if err != nil {
		a.state.SetCurrentStatus("Repair failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Repair database", strings.TrimSpace(path), err)
		return engine.RepairReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Repair created at %q", path))
	a.recordSuccessActivity("Repair database", strings.TrimSpace(path), fmt.Sprintf("Recovered %d record(s)", report.RecoveredRecords))
	return report, nil
}

// OpenDataFolder launches the operating system file manager at the data folder.
func (a *Application) OpenDataFolder() error {
	a.state.SetCurrentStatus("Opening data folder...")

	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer", a.db.DataDir())
	case "darwin":
		command = exec.Command("open", a.db.DataDir())
	default:
		command = exec.Command("xdg-open", a.db.DataDir())
	}

	if err := command.Start(); err != nil {
		a.state.SetCurrentStatus("Open folder failed")
		a.state.SetLastResult(err.Error())
		a.recordFailureActivity("Open data folder", a.db.DataDir(), err)
		return fmt.Errorf("open data folder: %w", err)
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Opened data folder")
	a.recordSuccessActivity("Open data folder", a.db.DataDir(), "Opened application data directory")
	return nil
}

func (a *Application) recordSuccessActivity(action, target, detail string) {
	_ = a.activityStore.Append(ActivityEntry{
		Action: action,
		Target: strings.TrimSpace(target),
		Status: "success",
		Detail: strings.TrimSpace(detail),
	})
}

func (a *Application) recordFailureActivity(action, target string, err error) {
	if err == nil {
		return
	}

	_ = a.activityStore.Append(ActivityEntry{
		Action: action,
		Target: strings.TrimSpace(target),
		Status: "error",
		Detail: err.Error(),
	})
}

func normalizeActivityTarget(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, " -> ")
}

func formatHistoryEntry(input, result string, err error) string {
	lines := []string{
		"[" + time.Now().Format("2006-01-02 15:04:05") + "]",
		"> " + strings.TrimSpace(input),
	}

	if err != nil {
		lines = append(lines, "ERROR: "+err.Error())
	} else {
		lines = append(lines, result)
	}

	return strings.Join(lines, "\n")
}

func parsePresetPathCommand(commandText, verb string) (string, string, error) {
	rest := strings.TrimSpace(commandText[len(verb):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", "", err
	}
	if len(args) != 2 {
		return "", "", fmt.Errorf("%s requires preset name and path", verb)
	}
	if strings.TrimSpace(args[0]) == "" {
		return "", "", fmt.Errorf("%s preset name must not be empty", verb)
	}
	if strings.TrimSpace(args[1]) == "" {
		return "", "", fmt.Errorf("%s path must not be empty", verb)
	}
	return strings.TrimSpace(args[0]), strings.TrimSpace(args[1]), nil
}

func parseImportPresetCommand(commandText string) (string, string, bool, error) {
	rest := strings.TrimSpace(commandText[len("IMPORTPRESET"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", "", false, err
	}
	if len(args) < 2 || len(args) > 3 {
		return "", "", false, fmt.Errorf("IMPORTPRESET requires preset name, path, and optional dry-run")
	}
	presetName := strings.TrimSpace(args[0])
	sourcePath := strings.TrimSpace(args[1])
	dryRun := false
	if len(args) == 3 {
		if !strings.EqualFold(strings.TrimSpace(args[2]), "dry-run") {
			return "", "", false, fmt.Errorf("unexpected IMPORTPRESET argument %q", args[2])
		}
		dryRun = true
	}
	if presetName == "" {
		return "", "", false, fmt.Errorf("IMPORTPRESET preset name must not be empty")
	}
	if sourcePath == "" {
		return "", "", false, fmt.Errorf("IMPORTPRESET path must not be empty")
	}
	return presetName, sourcePath, dryRun, nil
}

func parseShowPresetCommand(commandText string) (string, error) {
	rest := strings.TrimSpace(commandText[len("SHOWPRESET"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", fmt.Errorf("SHOWPRESET requires exactly one preset name")
	}
	presetName := strings.TrimSpace(args[0])
	if presetName == "" {
		return "", fmt.Errorf("SHOWPRESET preset name must not be empty")
	}
	return presetName, nil
}

func parseDeletePresetCommand(commandText string) (string, error) {
	rest := strings.TrimSpace(commandText[len("DELETEPRESET"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", fmt.Errorf("DELETEPRESET requires exactly one preset name")
	}
	presetName := strings.TrimSpace(args[0])
	if presetName == "" {
		return "", fmt.Errorf("DELETEPRESET preset name must not be empty")
	}
	return presetName, nil
}

func parseSavePresetCommand(commandText string) (DatasetPreset, error) {
	rest := strings.TrimSpace(commandText[len("SAVEPRESET"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return DatasetPreset{}, err
	}
	if len(args) < 4 || len(args) > 5 {
		return DatasetPreset{}, fmt.Errorf("SAVEPRESET requires name, collection, key field, conflict mode, and optional query")
	}

	preset := DatasetPreset{
		Name:         strings.TrimSpace(args[0]),
		Collection:   strings.TrimSpace(args[1]),
		KeyField:     strings.TrimSpace(args[2]),
		ConflictMode: strings.TrimSpace(strings.ToLower(args[3])),
	}
	if len(args) == 5 {
		preset.QueryText = strings.TrimSpace(args[4])
	}

	if preset.Name == "" {
		return DatasetPreset{}, fmt.Errorf("SAVEPRESET preset name must not be empty")
	}
	if preset.Collection == "" {
		return DatasetPreset{}, fmt.Errorf("SAVEPRESET collection must not be empty")
	}
	if preset.KeyField == "" {
		return DatasetPreset{}, fmt.Errorf("SAVEPRESET key field must not be empty")
	}
	switch preset.ConflictMode {
	case "skip", "overwrite":
	default:
		return DatasetPreset{}, fmt.Errorf("SAVEPRESET conflict mode must be skip or overwrite")
	}

	return preset, nil
}

func parseRenamePresetCommand(commandText string) (string, string, error) {
	rest := strings.TrimSpace(commandText[len("RENAMEDPRESET"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", "", err
	}
	if len(args) != 2 {
		return "", "", fmt.Errorf("RENAMEDPRESET requires old name and new name")
	}

	oldName := strings.TrimSpace(args[0])
	newName := strings.TrimSpace(args[1])
	if oldName == "" {
		return "", "", fmt.Errorf("RENAMEDPRESET old preset name must not be empty")
	}
	if newName == "" {
		return "", "", fmt.Errorf("RENAMEDPRESET new preset name must not be empty")
	}
	return oldName, newName, nil
}

func parseDuplicatePresetCommand(commandText string) (string, string, error) {
	rest := strings.TrimSpace(commandText[len("DUPLICATEPRESET"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", "", err
	}
	if len(args) != 2 {
		return "", "", fmt.Errorf("DUPLICATEPRESET requires source name and new name")
	}

	sourceName := strings.TrimSpace(args[0])
	newName := strings.TrimSpace(args[1])
	if sourceName == "" {
		return "", "", fmt.Errorf("DUPLICATEPRESET source preset name must not be empty")
	}
	if newName == "" {
		return "", "", fmt.Errorf("DUPLICATEPRESET new preset name must not be empty")
	}
	return sourceName, newName, nil
}

func parsePresetConfigImportCommand(commandText string) (string, error) {
	rest := strings.TrimSpace(commandText[len("IMPORTPRESETCONFIG"):])
	args, err := tokenizeAppCommandArguments(rest)
	if err != nil {
		return "", err
	}
	if len(args) != 1 {
		return "", fmt.Errorf("IMPORTPRESETCONFIG requires exactly one path")
	}

	sourcePath := strings.TrimSpace(args[0])
	if sourcePath == "" {
		return "", fmt.Errorf("IMPORTPRESETCONFIG path must not be empty")
	}
	return sourcePath, nil
}

func tokenizeAppCommandArguments(input string) ([]string, error) {
	tokens := make([]string, 0, 4)
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

	for _, r := range input {
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
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flushCurrent()
		default:
			current.WriteRune(r)
		}
	}

	if escaping {
		return nil, fmt.Errorf("invalid command arguments: unterminated escape sequence")
	}
	if inQuotes {
		return nil, fmt.Errorf("invalid command arguments: missing closing quote")
	}

	flushCurrent()
	for index, token := range tokens {
		if strings.HasPrefix(token, "\"") && strings.HasSuffix(token, "\"") && len(token) >= 2 {
			inner := token[1 : len(token)-1]
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			inner = strings.ReplaceAll(inner, `\\`, `\`)
			tokens[index] = inner
		}
	}

	return tokens, nil
}

func formatPresetPreviewResult(preset DatasetPreset, report engine.ImportPreviewReport) string {
	return strings.Join([]string{
		"preset=" + preset.Name,
		"collection=" + report.Collection,
		"key_field=" + report.KeyField,
		"conflict_mode=" + report.ConflictMode,
		"total_lines=" + strconv.Itoa(report.TotalLines),
		"valid_documents=" + strconv.Itoa(report.ValidDocuments),
		"new_records=" + strconv.Itoa(report.NewRecordCount),
		"overwrite_candidates=" + strconv.Itoa(report.OverwriteCount),
		"skipped_by_mode=" + strconv.Itoa(report.SkipCount),
	}, "\n")
}

func formatPresetImportResult(preset DatasetPreset, report engine.ImportReport) string {
	return strings.Join([]string{
		"preset=" + preset.Name,
		"collection=" + report.Collection,
		"key_field=" + report.KeyField,
		"conflict_mode=" + report.ConflictMode,
		"dry_run=" + strconv.FormatBool(report.DryRun),
		"imported_records=" + strconv.Itoa(report.ImportedRecords),
		"skipped_records=" + strconv.Itoa(report.SkippedRecords),
	}, "\n")
}

func formatPresetExportResult(preset DatasetPreset, report engine.ExportReport) string {
	lines := []string{
		"preset=" + preset.Name,
		"collection=" + report.Collection,
		"destination=" + report.DestinationPath,
		"exported_records=" + strconv.Itoa(report.ExportedRecords),
	}
	if strings.TrimSpace(report.QueryText) != "" {
		lines = append(lines, "query="+report.QueryText)
	}
	return strings.Join(lines, "\n")
}

func formatPresetListResult(presets []DatasetPreset) string {
	if len(presets) == 0 {
		return "preset_count=0"
	}

	lines := []string{"preset_count=" + strconv.Itoa(len(presets))}
	for _, preset := range presets {
		lines = append(lines, "preset="+preset.Name)
	}
	return strings.Join(lines, "\n")
}

func formatShowPresetResult(preset DatasetPreset) string {
	lines := []string{
		"name=" + preset.Name,
		"collection=" + preset.Collection,
		"key_field=" + preset.KeyField,
		"conflict_mode=" + preset.ConflictMode,
	}
	if strings.TrimSpace(preset.QueryText) != "" {
		lines = append(lines, "query="+preset.QueryText)
	}
	return strings.Join(lines, "\n")
}

func formatSavedPresetResult(preset DatasetPreset) string {
	lines := []string{
		"saved_preset=" + preset.Name,
		"collection=" + preset.Collection,
		"key_field=" + preset.KeyField,
		"conflict_mode=" + preset.ConflictMode,
	}
	if strings.TrimSpace(preset.QueryText) != "" {
		lines = append(lines, "query="+preset.QueryText)
	}
	return strings.Join(lines, "\n")
}

func formatDeletedPresetResult(name string) string {
	return "deleted_preset=" + strings.TrimSpace(name)
}

func formatRenamedPresetResult(oldName, newName string) string {
	return strings.Join([]string{
		"renamed_preset=" + strings.TrimSpace(oldName),
		"new_name=" + strings.TrimSpace(newName),
	}, "\n")
}

func formatDuplicatedPresetResult(sourceName, newName string) string {
	return strings.Join([]string{
		"duplicated_preset=" + strings.TrimSpace(sourceName),
		"new_name=" + strings.TrimSpace(newName),
	}, "\n")
}

func formatExportedPresetConfigResult(preset DatasetPreset, destinationPath string) string {
	lines := []string{
		"exported_preset=" + preset.Name,
		"collection=" + preset.Collection,
		"destination=" + strings.TrimSpace(destinationPath),
	}
	return strings.Join(lines, "\n")
}

func formatImportedPresetConfigResult(preset DatasetPreset, sourcePath string) string {
	lines := []string{
		"imported_preset=" + preset.Name,
		"collection=" + preset.Collection,
		"source=" + strings.TrimSpace(sourcePath),
	}
	return strings.Join(lines, "\n")
}
