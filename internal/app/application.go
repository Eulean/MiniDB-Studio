package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"minidb-studio/internal/engine"
)

// Application coordinates the database engine and the desktop UI state.
type Application struct {
	db    *engine.DB
	state *State
}

// NewApplication opens MiniDB and prepares the shared application state.
func NewApplication() (*Application, error) {
	db, err := engine.Open()
	if err != nil {
		return nil, err
	}

	return &Application{
		db:    db,
		state: NewState(db.DataDir()),
	}, nil
}

// Close releases database resources when the desktop window exits.
func (a *Application) Close() error {
	return a.db.Close()
}

// State exposes the shared UI state to window components.
func (a *Application) State() *State {
	return a.state
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
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Saved record %q/%q", collection, key))
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
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Renamed record %q/%q to %q/%q", oldCollection, oldKey, newCollection, newKey))
	return nil
}

// DeleteRecord removes a key from the database and updates status text.
func (a *Application) DeleteRecord(collection, key string) error {
	a.state.SetCurrentStatus("Deleting record...")

	if err := a.db.DeleteFromCollection(collection, key); err != nil {
		a.state.SetCurrentStatus("Delete failed")
		a.state.SetLastResult(err.Error())
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Deleted record %q/%q", collection, key))
	return nil
}

// ExecuteCommand runs one MiniDB v1 console command and stores history output.
func (a *Application) ExecuteCommand(input string) (string, error) {
	a.state.SetCurrentStatus("Running command...")

	result, err := a.db.Execute(input)
	historyEntry := formatHistoryEntry(input, result, err)
	a.state.AppendHistory(historyEntry)

	if err != nil {
		a.state.SetCurrentStatus("Command failed")
		a.state.SetLastResult(err.Error())
		return "", err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Command completed")
	return result, nil
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
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Snapshot created")
	return nil
}

// Validate inspects the snapshot and segment files and returns the validation report.
func (a *Application) Validate() (engine.ValidationReport, error) {
	a.state.SetCurrentStatus("Validating database...")

	report, err := a.db.Validate()
	if err != nil {
		a.state.SetCurrentStatus("Validation failed")
		a.state.SetLastResult(err.Error())
		return engine.ValidationReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Validation completed")
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
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Database compaction completed")
	return nil
}

// BackupToPath writes a copy of the current durable log to the selected path.
func (a *Application) BackupToPath(path string) error {
	a.state.SetCurrentStatus("Creating backup...")

	if err := a.db.BackupTo(path); err != nil {
		a.state.SetCurrentStatus("Backup failed")
		a.state.SetLastResult(err.Error())
		return err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Backup created at %q", path))
	return nil
}

// ExportCollection writes one collection or the full database to a chosen destination file.
func (a *Application) ExportCollection(collection, path string) (engine.ExportReport, error) {
	a.state.SetCurrentStatus("Exporting data...")

	report, err := a.db.ExportCollection(collection, path)
	if err != nil {
		a.state.SetCurrentStatus("Export failed")
		a.state.SetLastResult(err.Error())
		return engine.ExportReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Export created at %q", path))
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
		return engine.RepairReport{}, err
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult(fmt.Sprintf("Repair created at %q", path))
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
		return fmt.Errorf("open data folder: %w", err)
	}

	a.state.SetCurrentStatus("Ready")
	a.state.SetLastResult("Opened data folder")
	return nil
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
