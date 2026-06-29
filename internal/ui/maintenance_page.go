package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	studioapp "minidb-studio/internal/app"
	"minidb-studio/internal/engine"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynestorage "fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

// MaintenancePage owns stats display and durable maintenance actions.
type MaintenancePage struct {
	window               fyne.Window
	application          *studioapp.Application
	onStatusChanged      func()
	root                 fyne.CanvasObject
	healthValue          *widget.Label
	recommendationsValue *widget.Label
	liveKeysValue        *widget.Label
	liveDataValue        *widget.Label
	activeSizeValue      *widget.Label
	segmentCountValue    *widget.Label
	snapshotCountValue   *widget.Label
	replayCountValue     *widget.Label
	setOpsValue          *widget.Label
	deleteOpsValue       *widget.Label
	snapshotTimeValue    *widget.Label
	compactTimeValue     *widget.Label
}

type datasetPresetEditor struct {
	application    *studioapp.Application
	presetSelect   *widget.Select
	presetName     *widget.Entry
	collection     *widget.Entry
	keyField       *widget.Entry
	conflictSelect *widget.Select
	query          *widget.Entry
}

type maintenanceDialogConfig struct {
	title       string
	confirmText string
	body        fyne.CanvasObject
	size        fyne.Size
	onConfirm   func()
}

// NewMaintenancePage builds the maintenance tools and stats display.
func NewMaintenancePage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *MaintenancePage {
	page := &MaintenancePage{
		window:               window,
		application:          application,
		onStatusChanged:      onStatusChanged,
		healthValue:          widget.NewLabel(""),
		recommendationsValue: widget.NewLabel(""),
		liveKeysValue:        widget.NewLabel(""),
		liveDataValue:        widget.NewLabel(""),
		activeSizeValue:      widget.NewLabel(""),
		segmentCountValue:    widget.NewLabel(""),
		snapshotCountValue:   widget.NewLabel(""),
		replayCountValue:     widget.NewLabel(""),
		setOpsValue:          widget.NewLabel(""),
		deleteOpsValue:       widget.NewLabel(""),
		snapshotTimeValue:    widget.NewLabel(""),
		compactTimeValue:     widget.NewLabel(""),
	}

	refreshButton := widget.NewButton("Refresh Statistics", func() {
		page.Refresh()
		application.State().SetLastResult("Statistics refreshed")
		onStatusChanged()
	})

	compactButton := widget.NewButton("Compact Database", func() {
		if err := application.Compact(); err != nil {
			dialog.ShowError(err, window)
			page.Refresh()
			onStatusChanged()
			return
		}

		page.Refresh()
		onStatusChanged()
	})

	snapshotButton := widget.NewButton("Create Snapshot", func() {
		if err := application.Snapshot(); err != nil {
			dialog.ShowError(err, window)
			page.Refresh()
			onStatusChanged()
			return
		}

		page.Refresh()
		onStatusChanged()
	})

	validateButton := widget.NewButton("Validate Database", func() {
		report, err := application.Validate()
		if err != nil {
			dialog.ShowError(err, window)
			onStatusChanged()
			return
		}

		dialog.ShowInformation("Validation Report", formatValidationReport(report), window)
		onStatusChanged()
	})

	repairButton := widget.NewButton("Repair Database", func() {
		folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, window)
				return
			}
			if uri == nil {
				return
			}

			report, err := application.RepairTo(uri.Path())
			if err != nil {
				dialog.ShowError(err, window)
				onStatusChanged()
				return
			}

			dialog.ShowInformation("Repair Report", formatRepairReport(report), window)
			onStatusChanged()
		}, window)
		folderDialog.Show()
	})

	backupButton := widget.NewButton("Create Backup", func() {
		saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, window)
				return
			}
			if writer == nil {
				return
			}

			path := writer.URI().Path()
			writer.Close()

			if err := application.BackupToPath(path); err != nil {
				dialog.ShowError(err, window)
				onStatusChanged()
				return
			}

			onStatusChanged()
		}, window)

		saveDialog.SetFileName(filepath.Base(application.DataDir()) + "-backup.zip")
		saveDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".zip"}))
		saveDialog.Show()
	})

	exportButton := widget.NewButton("Export Data", func() {
		presetSelect := widget.NewSelect([]string{}, nil)
		presetNameEntry := widget.NewEntry()
		presetNameEntry.SetPlaceHolder("Preset name")
		collectionEntry := widget.NewEntry()
		collectionEntry.SetPlaceHolder("Leave blank for all collections")
		queryEntry := widget.NewEntry()
		queryEntry.SetPlaceHolder("Optional JSON query for one collection")
		presetEditor := newDatasetPresetEditor(application, presetSelect, presetNameEntry, collectionEntry, nil, nil, queryEntry)
		presetButtons := container.NewGridWithColumns(
			3,
			widget.NewButton("Load Preset", func() {
				if err := presetEditor.applySelected(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
			widget.NewButton("Save Preset", func() {
				if err := presetEditor.saveCurrent(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
			widget.NewButton("Delete Preset", func() {
				if err := presetEditor.deleteSelected(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
		)
		formBody := container.NewVBox(
			widget.NewLabel("Preset"),
			presetSelect,
			widget.NewLabel("Preset Name"),
			presetNameEntry,
			presetButtons,
			widget.NewSeparator(),
			widget.NewLabel("Collection"),
			collectionEntry,
			widget.NewSeparator(),
			widget.NewLabel("JSON Query"),
			queryEntry,
		)
		showMaintenanceDialog(window, maintenanceDialogConfig{
			title:       "Export Collection",
			confirmText: "Continue",
			size:        fyne.NewSize(720, 560),
			body: sectionCard(
				"Export Dataset",
				"Export an entire collection or a filtered JSON slice to a portable `.jsonl` file.",
				formBody,
			),
			onConfirm: func() {
				saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
					if err != nil {
						dialog.ShowError(err, window)
						return
					}
					if writer == nil {
						return
					}

					path := writer.URI().Path()
					writer.Close()

					var (
						report    engine.ExportReport
						exportErr error
					)
					if strings.TrimSpace(queryEntry.Text) == "" {
						report, exportErr = application.ExportCollection(collectionEntry.Text, path)
					} else {
						report, exportErr = application.ExportJSONQueryCollection(collectionEntry.Text, queryEntry.Text, path)
					}
					if exportErr != nil {
						dialog.ShowError(exportErr, window)
						onStatusChanged()
						return
					}

					dialog.ShowInformation("Export Report", formatExportReport(report), window)
					onStatusChanged()
				}, window)
				saveDialog.SetFileName("minidb-export.jsonl")
				saveDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".jsonl"}))
				saveDialog.Show()
			},
		})
	})

	importButton := widget.NewButton("Import NDJSON", func() {
		sourcePathEntry := widget.NewEntry()
		sourcePathEntry.SetPlaceHolder("Choose an .ndjson or .jsonl file")

		browseButton := widget.NewButton("Browse File", func() {
			openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil {
					dialog.ShowError(err, window)
					return
				}
				if reader == nil {
					return
				}

				sourcePathEntry.SetText(reader.URI().Path())
				reader.Close()
			}, window)
			openDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".ndjson", ".jsonl"}))
			openDialog.Show()
		})

		presetSelect := widget.NewSelect([]string{}, nil)
		presetNameEntry := widget.NewEntry()
		presetNameEntry.SetPlaceHolder("Preset name")
		collectionEntry := widget.NewEntry()
		collectionEntry.SetText("docs")
		keyFieldEntry := widget.NewEntry()
		keyFieldEntry.SetText("id")
		conflictSelect := widget.NewSelect([]string{engine.ImportConflictSkip, engine.ImportConflictOverwrite}, nil)
		conflictSelect.SetSelected(engine.ImportConflictSkip)
		presetEditor := newDatasetPresetEditor(application, presetSelect, presetNameEntry, collectionEntry, keyFieldEntry, conflictSelect, nil)
		presetButtons := container.NewGridWithColumns(
			3,
			widget.NewButton("Load Preset", func() {
				if err := presetEditor.applySelected(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
			widget.NewButton("Save Preset", func() {
				if err := presetEditor.saveCurrent(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
			widget.NewButton("Delete Preset", func() {
				if err := presetEditor.deleteSelected(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
		)

		formBody := container.NewVBox(
			widget.NewLabel("Preset"),
			presetSelect,
			widget.NewLabel("Preset Name"),
			presetNameEntry,
			presetButtons,
			widget.NewSeparator(),
			widget.NewLabel("Source File"),
			sourcePathEntry,
			browseButton,
			widget.NewSeparator(),
			widget.NewLabel("Collection"),
			collectionEntry,
			widget.NewSeparator(),
			widget.NewLabel("Key Field"),
			keyFieldEntry,
			widget.NewSeparator(),
			widget.NewLabel("Conflict Mode"),
			conflictSelect,
		)

		showMaintenanceDialog(window, maintenanceDialogConfig{
			title:       "Import NDJSON",
			confirmText: "Import",
			size:        fyne.NewSize(760, 620),
			body: sectionCard(
				"Import Dataset",
				"Preview and then durably import NDJSON documents into one collection with preset-aware defaults.",
				formBody,
			),
			onConfirm: func() {
				preview, err := application.PreviewNDJSONImport(collectionEntry.Text, sourcePathEntry.Text, keyFieldEntry.Text, conflictSelect.Selected)
				if err != nil {
					dialog.ShowError(err, window)
					onStatusChanged()
					return
				}

				confirmImportAfterPreview(window, application, page, onStatusChanged, preview)
			},
		})
	})

	previewButton := widget.NewButton("Preview NDJSON Import", func() {
		sourcePathEntry := widget.NewEntry()
		sourcePathEntry.SetPlaceHolder("Choose an .ndjson or .jsonl file")

		browseButton := widget.NewButton("Browse File", func() {
			openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err != nil {
					dialog.ShowError(err, window)
					return
				}
				if reader == nil {
					return
				}

				sourcePathEntry.SetText(reader.URI().Path())
				reader.Close()
			}, window)
			openDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".ndjson", ".jsonl"}))
			openDialog.Show()
		})

		presetSelect := widget.NewSelect([]string{}, nil)
		presetNameEntry := widget.NewEntry()
		presetNameEntry.SetPlaceHolder("Preset name")
		collectionEntry := widget.NewEntry()
		collectionEntry.SetText("docs")
		keyFieldEntry := widget.NewEntry()
		keyFieldEntry.SetText("id")
		conflictSelect := widget.NewSelect([]string{engine.ImportConflictSkip, engine.ImportConflictOverwrite}, nil)
		conflictSelect.SetSelected(engine.ImportConflictSkip)
		presetEditor := newDatasetPresetEditor(application, presetSelect, presetNameEntry, collectionEntry, keyFieldEntry, conflictSelect, nil)
		presetButtons := container.NewGridWithColumns(
			3,
			widget.NewButton("Load Preset", func() {
				if err := presetEditor.applySelected(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
			widget.NewButton("Save Preset", func() {
				if err := presetEditor.saveCurrent(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
			widget.NewButton("Delete Preset", func() {
				if err := presetEditor.deleteSelected(); err != nil {
					dialog.ShowError(err, window)
				}
			}),
		)

		formBody := container.NewVBox(
			widget.NewLabel("Preset"),
			presetSelect,
			widget.NewLabel("Preset Name"),
			presetNameEntry,
			presetButtons,
			widget.NewSeparator(),
			widget.NewLabel("Source File"),
			sourcePathEntry,
			browseButton,
			widget.NewSeparator(),
			widget.NewLabel("Collection"),
			collectionEntry,
			widget.NewSeparator(),
			widget.NewLabel("Key Field"),
			keyFieldEntry,
			widget.NewSeparator(),
			widget.NewLabel("Conflict Mode"),
			conflictSelect,
		)

		showMaintenanceDialog(window, maintenanceDialogConfig{
			title:       "Preview NDJSON Import",
			confirmText: "Preview",
			size:        fyne.NewSize(760, 620),
			body: sectionCard(
				"Preview Dataset Import",
				"Inspect schema, conflicts, and record counts before writing anything to disk.",
				formBody,
			),
			onConfirm: func() {
				report, err := application.PreviewNDJSONImport(collectionEntry.Text, sourcePathEntry.Text, keyFieldEntry.Text, conflictSelect.Selected)
				if err != nil {
					dialog.ShowError(err, window)
					onStatusChanged()
					return
				}

				dialog.ShowInformation("Import Preview", formatImportPreviewReport(report), window)
				onStatusChanged()
			},
		})
	})

	openFolderButton := widget.NewButton("Open Data Folder", func() {
		if err := application.OpenDataFolder(); err != nil {
			dialog.ShowError(err, window)
			onStatusChanged()
			return
		}

		onStatusChanged()
	})

	statsGrid := container.NewGridWithColumns(
		2,
		widget.NewLabelWithStyle("Live key count", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.liveKeysValue,
		widget.NewLabelWithStyle("Live data size", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.liveDataValue,
		widget.NewLabelWithStyle("Active segment size", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.activeSizeValue,
		widget.NewLabelWithStyle("Segment count", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.segmentCountValue,
		widget.NewLabelWithStyle("Snapshot count", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.snapshotCountValue,
		widget.NewLabelWithStyle("Startup replay count", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.replayCountValue,
		widget.NewLabelWithStyle("Total set operations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.setOpsValue,
		widget.NewLabelWithStyle("Total delete operations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.deleteOpsValue,
		widget.NewLabelWithStyle("Last snapshot time", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.snapshotTimeValue,
		widget.NewLabelWithStyle("Last compaction time", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.compactTimeValue,
	)

	healthCard := sectionCard(
		"Health And Recommendations",
		"See whether the local database is healthy and which maintenance steps are worth running next.",
		container.NewVBox(
			widget.NewLabelWithStyle("Health", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			page.healthValue,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Recommendations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			page.recommendationsValue,
		),
	)

	statsCard := sectionCard(
		"Database Statistics",
		"Core storage, replay, and operation metrics for the current MiniDB workspace.",
		statsGrid,
	)

	actionsCard := sectionCard(
		"Maintenance Actions",
		"Run safe operations for snapshotting, validation, repair, backup, import, export, and compaction.",
		container.NewGridWithColumns(
			2,
			refreshButton,
			snapshotButton,
			validateButton,
			repairButton,
			compactButton,
			exportButton,
			previewButton,
			importButton,
			backupButton,
			openFolderButton,
		),
	)

	leftColumn := container.NewVBox(healthCard, statsCard)
	rightColumn := container.NewVBox(actionsCard)
	split := container.NewHSplit(leftColumn, rightColumn)
	split.Offset = 0.62

	page.root = standardScroll(split)

	page.recommendationsValue.Wrapping = fyne.TextWrapWord
	page.healthValue.Wrapping = fyne.TextWrapWord

	page.Refresh()
	return page
}

// CanvasObject returns the root maintenance view for embedding in the main window.
func (p *MaintenancePage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads the database statistics labels.
func (p *MaintenancePage) Refresh() {
	maintenanceReport, reportErr := p.application.MaintenanceReport()
	if reportErr != nil {
		p.healthValue.SetText("Unavailable")
		p.recommendationsValue.SetText(reportErr.Error())
	} else {
		if maintenanceReport.Healthy {
			p.healthValue.SetText("Healthy")
		} else {
			p.healthValue.SetText("Needs Attention")
		}
		p.recommendationsValue.SetText(strings.Join(maintenanceReport.Recommendations, "\n"))
	}

	stats, err := p.application.Stats()
	if err != nil {
		p.liveKeysValue.SetText("Unavailable")
		p.liveDataValue.SetText("Unavailable")
		p.activeSizeValue.SetText("Unavailable")
		p.segmentCountValue.SetText("Unavailable")
		p.snapshotCountValue.SetText("Unavailable")
		p.replayCountValue.SetText("Unavailable")
		p.setOpsValue.SetText("Unavailable")
		p.deleteOpsValue.SetText("Unavailable")
		p.snapshotTimeValue.SetText("Unavailable")
		p.compactTimeValue.SetText(err.Error())
		return
	}

	p.liveKeysValue.SetText(fmt.Sprintf("%d", stats.LiveKeyCount))
	p.liveDataValue.SetText(fmt.Sprintf("%d bytes", stats.TotalLiveDataSize))
	p.activeSizeValue.SetText(fmt.Sprintf("%d bytes", stats.ActiveSegmentSize))
	p.segmentCountValue.SetText(fmt.Sprintf("%d", stats.SegmentCount))
	p.snapshotCountValue.SetText(fmt.Sprintf("%d", stats.SnapshotCount))
	p.replayCountValue.SetText(fmt.Sprintf("%d", stats.StartupReplayCount))
	p.setOpsValue.SetText(fmt.Sprintf("%d", stats.TotalSetOperations))
	p.deleteOpsValue.SetText(fmt.Sprintf("%d", stats.TotalDeleteOps))
	if stats.LastSnapshotTime.IsZero() {
		p.snapshotTimeValue.SetText("Never")
	} else {
		p.snapshotTimeValue.SetText(stats.LastSnapshotTime.Local().Format("2006-01-02 15:04:05"))
	}
	if stats.LastCompactionTime.IsZero() {
		p.compactTimeValue.SetText("Never")
	} else {
		p.compactTimeValue.SetText(stats.LastCompactionTime.Local().Format("2006-01-02 15:04:05"))
	}
}

func formatExportReport(report engine.ExportReport) string {
	lines := []string{
		fmt.Sprintf("Destination: %s", report.DestinationPath),
		fmt.Sprintf("Collection: %s", report.Collection),
		fmt.Sprintf("Exported Records: %d", report.ExportedRecords),
	}
	if strings.TrimSpace(report.QueryText) != "" {
		lines = append(lines, "Query: "+report.QueryText)
	}
	return strings.Join(lines, "\n")
}

func formatRepairReport(report engine.RepairReport) string {
	lines := []string{
		fmt.Sprintf("Destination: %s", report.DestinationDir),
		fmt.Sprintf("Recovered Records: %d", report.RecoveredRecords),
		fmt.Sprintf("Used Snapshot: %t", report.UsedSnapshot),
	}
	if len(report.Warnings) > 0 {
		lines = append(lines, "Warnings: "+strings.Join(report.Warnings, " | "))
	}
	return strings.Join(lines, "\n")
}

func formatImportReport(report engine.ImportReport) string {
	return strings.Join([]string{
		fmt.Sprintf("Source: %s", report.SourcePath),
		fmt.Sprintf("Collection: %s", report.Collection),
		fmt.Sprintf("Key Field: %s", report.KeyField),
		fmt.Sprintf("Conflict Mode: %s", report.ConflictMode),
		fmt.Sprintf("Dry Run: %t", report.DryRun),
		fmt.Sprintf("Imported Records: %d", report.ImportedRecords),
		fmt.Sprintf("Skipped Records: %d", report.SkippedRecords),
	}, "\n")
}

func formatImportPreviewReport(report engine.ImportPreviewReport) string {
	lines := []string{
		fmt.Sprintf("Source: %s", report.SourcePath),
		fmt.Sprintf("Collection: %s", report.Collection),
		fmt.Sprintf("Key Field: %s", report.KeyField),
		fmt.Sprintf("Conflict Mode: %s", report.ConflictMode),
		fmt.Sprintf("Total Non-Empty Lines: %d", report.TotalLines),
		fmt.Sprintf("Valid Documents: %d", report.ValidDocuments),
		fmt.Sprintf("Invalid Lines: %d", report.InvalidLines),
		fmt.Sprintf("Missing Key Count: %d", report.MissingKeyCount),
		fmt.Sprintf("Duplicate Keys In File: %d", report.DuplicateKeysInFile),
		fmt.Sprintf("Existing Key Conflicts: %d", report.ExistingKeyConflicts),
		fmt.Sprintf("New Records: %d", report.NewRecordCount),
		fmt.Sprintf("Overwrite Candidates: %d", report.OverwriteCount),
		fmt.Sprintf("Skipped By Mode: %d", report.SkipCount),
		fmt.Sprintf("Distinct Fields Shown: %d", report.TotalDistinctFields),
		"Sample Keys: " + strings.Join(report.SampleKeys, ", "),
	}
	if len(report.ChangeSamples) > 0 {
		lines = append(lines, "Change Samples:")
		for _, sample := range report.ChangeSamples {
			lines = append(lines, fmt.Sprintf("- %s [%s]", sample.Key, sample.Status))
			if sample.CurrentPreview != "" {
				lines = append(lines, "  current: "+sample.CurrentPreview)
			}
			if sample.IncomingPreview != "" {
				lines = append(lines, "  incoming: "+sample.IncomingPreview)
			}
			if len(sample.AddedFields) > 0 {
				lines = append(lines, "  added: "+strings.Join(sample.AddedFields, ", "))
			}
			if len(sample.RemovedFields) > 0 {
				lines = append(lines, "  removed: "+strings.Join(sample.RemovedFields, ", "))
			}
			if len(sample.ChangedFields) > 0 {
				lines = append(lines, "  changed: "+strings.Join(sample.ChangedFields, ", "))
			}
		}
	}
	if len(report.FieldSummaries) > 0 {
		lines = append(lines, "Field Summaries:")
		for _, field := range report.FieldSummaries {
			lines = append(lines, fmt.Sprintf("- %s (%d) [%s]", field.Path, field.ObservedCount, strings.Join(field.Types, ", ")))
		}
	}
	if len(report.FirstProblems) > 0 {
		lines = append(lines, "Problems: "+strings.Join(report.FirstProblems, " | "))
	}
	return strings.Join(lines, "\n")
}

func confirmImportAfterPreview(window fyne.Window, application *studioapp.Application, page *MaintenancePage, onStatusChanged func(), preview engine.ImportPreviewReport) {
	message := formatImportPreviewReport(preview)
	if preview.InvalidLines > 0 || preview.MissingKeyCount > 0 {
		dialog.ShowInformation("Import Blocked", message, window)
		onStatusChanged()
		return
	}

	dialog.ShowConfirm("Import NDJSON", message+"\n\nContinue with durable import?", func(confirmed bool) {
		if !confirmed {
			return
		}

		report, err := application.ImportNDJSON(preview.Collection, preview.SourcePath, preview.KeyField, preview.ConflictMode, false)
		if err != nil {
			dialog.ShowError(err, window)
			onStatusChanged()
			return
		}

		page.Refresh()
		dialog.ShowInformation("Import Report", formatImportReport(report), window)
		onStatusChanged()
	}, window)
}

func newDatasetPresetEditor(
	application *studioapp.Application,
	presetSelect *widget.Select,
	presetName *widget.Entry,
	collection *widget.Entry,
	keyField *widget.Entry,
	conflictSelect *widget.Select,
	query *widget.Entry,
) *datasetPresetEditor {
	editor := &datasetPresetEditor{
		application:    application,
		presetSelect:   presetSelect,
		presetName:     presetName,
		collection:     collection,
		keyField:       keyField,
		conflictSelect: conflictSelect,
		query:          query,
	}
	editor.refreshOptions()
	return editor
}

func (e *datasetPresetEditor) refreshOptions() error {
	presets, err := e.application.ListDatasetPresets()
	if err != nil {
		return err
	}

	options := make([]string, 0, len(presets))
	for _, preset := range presets {
		options = append(options, preset.Name)
	}
	e.presetSelect.SetOptions(options)
	if len(options) == 0 {
		e.presetSelect.ClearSelected()
	} else if e.presetSelect.Selected == "" || !sliceContainsString(options, e.presetSelect.Selected) {
		e.presetSelect.SetSelected(options[0])
	}
	return nil
}

func (e *datasetPresetEditor) applySelected() error {
	name := strings.TrimSpace(e.presetSelect.Selected)
	if name == "" {
		return fmt.Errorf("select a preset first")
	}

	presets, err := e.application.ListDatasetPresets()
	if err != nil {
		return err
	}
	for _, preset := range presets {
		if preset.Name != name {
			continue
		}
		e.presetName.SetText(preset.Name)
		e.collection.SetText(preset.Collection)
		if e.keyField != nil {
			e.keyField.SetText(preset.KeyField)
		}
		if e.conflictSelect != nil {
			e.conflictSelect.SetSelected(preset.ConflictMode)
		}
		if e.query != nil {
			e.query.SetText(preset.QueryText)
		}
		return nil
	}

	return fmt.Errorf("preset %q was not found", name)
}

func (e *datasetPresetEditor) saveCurrent() error {
	preset := studioapp.DatasetPreset{
		Name:       strings.TrimSpace(e.presetName.Text),
		Collection: strings.TrimSpace(e.collection.Text),
	}
	if e.keyField != nil {
		preset.KeyField = strings.TrimSpace(e.keyField.Text)
	}
	if e.conflictSelect != nil {
		preset.ConflictMode = strings.TrimSpace(e.conflictSelect.Selected)
	}
	if e.query != nil {
		preset.QueryText = strings.TrimSpace(e.query.Text)
	}

	if err := e.application.SaveDatasetPreset(preset); err != nil {
		return err
	}
	if err := e.refreshOptions(); err != nil {
		return err
	}
	e.presetSelect.SetSelected(preset.Name)
	return nil
}

func (e *datasetPresetEditor) deleteSelected() error {
	name := strings.TrimSpace(e.presetSelect.Selected)
	if name == "" {
		name = strings.TrimSpace(e.presetName.Text)
	}
	if err := e.application.DeleteDatasetPreset(name); err != nil {
		return err
	}
	e.presetName.SetText("")
	if err := e.refreshOptions(); err != nil {
		return err
	}
	return nil
}

func sliceContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func formatValidationReport(report engine.ValidationReport) string {
	lines := []string{
		fmt.Sprintf("Snapshot Present: %t", report.SnapshotPresent),
		fmt.Sprintf("Snapshot Valid: %t", report.SnapshotValid),
		fmt.Sprintf("Snapshot Sequence: %d", report.SnapshotSequence),
		fmt.Sprintf("Segment Count: %d", report.SegmentCount),
		"Valid Segments: " + strings.Join(report.ValidSegments, ", "),
		"Incomplete Tail Segments: " + strings.Join(report.IncompleteTailSegments, ", "),
		"Corrupted Segments: " + strings.Join(report.CorruptedSegments, ", "),
	}
	if len(report.Warnings) > 0 {
		lines = append(lines, "Warnings: "+strings.Join(report.Warnings, " | "))
	}

	return strings.Join(lines, "\n")
}

func showMaintenanceDialog(window fyne.Window, config maintenanceDialogConfig) {
	formDialog := dialog.NewCustomConfirm(config.title, config.confirmText, "Cancel", standardScroll(config.body), func(confirmed bool) {
		if !confirmed {
			return
		}
		config.onConfirm()
	}, window)
	formDialog.Resize(config.size)
	formDialog.Show()
}
