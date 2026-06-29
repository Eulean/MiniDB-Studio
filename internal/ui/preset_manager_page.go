package ui

import (
	"fmt"
	"strings"

	studioapp "minidb-studio/internal/app"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynestorage "fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

// PresetManagerPage gives dataset presets a first-class management surface outside one-off dialogs.
type PresetManagerPage struct {
	window          fyne.Window
	application     *studioapp.Application
	onStatusChanged func()
	root            fyne.CanvasObject
	presetSelect    *widget.Select
	nameEntry       *widget.Entry
	collectionEntry *widget.Entry
	keyFieldEntry   *widget.Entry
	conflictSelect  *widget.Select
	queryEntry      *widget.Entry
	detailLabel     *widget.Label
	activityLabel   *widget.Label
}

// NewPresetManagerPage builds a dedicated preset-management workspace.
func NewPresetManagerPage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *PresetManagerPage {
	page := &PresetManagerPage{
		window:          window,
		application:     application,
		onStatusChanged: onStatusChanged,
		presetSelect:    widget.NewSelect([]string{}, nil),
		nameEntry:       widget.NewEntry(),
		collectionEntry: widget.NewEntry(),
		keyFieldEntry:   widget.NewEntry(),
		conflictSelect:  widget.NewSelect([]string{"skip", "overwrite"}, nil),
		queryEntry:      widget.NewMultiLineEntry(),
		detailLabel:     widget.NewLabel(""),
		activityLabel:   widget.NewLabel(""),
	}

	page.nameEntry.SetPlaceHolder("Preset name")
	page.collectionEntry.SetPlaceHolder("Collection name")
	page.keyFieldEntry.SetPlaceHolder("Key field")
	page.queryEntry.SetPlaceHolder("Optional JSON query")
	page.queryEntry.SetMinRowsVisible(6)
	page.conflictSelect.SetSelected("skip")
	page.detailLabel.Wrapping = fyne.TextWrapWord
	page.activityLabel.Wrapping = fyne.TextWrapWord

	page.presetSelect.OnChanged = func(string) {
		if err := page.loadSelectedPreset(); err != nil {
			dialog.ShowError(err, page.window)
		}
	}

	form := container.NewVBox(
		widget.NewLabel("Preset"),
		page.presetSelect,
		widget.NewSeparator(),
		widget.NewLabel("Name"),
		page.nameEntry,
		widget.NewSeparator(),
		widget.NewLabel("Collection"),
		page.collectionEntry,
		widget.NewSeparator(),
		widget.NewLabel("Key Field"),
		page.keyFieldEntry,
		widget.NewSeparator(),
		widget.NewLabel("Conflict Mode"),
		page.conflictSelect,
		widget.NewSeparator(),
		widget.NewLabel("Query"),
		page.queryEntry,
	)

	actions := container.NewGridWithColumns(
		3,
		widget.NewButton("Save", func() {
			if err := page.savePreset(); err != nil {
				dialog.ShowError(err, page.window)
				return
			}
			page.Refresh()
			onStatusChanged()
		}),
		widget.NewButton("Duplicate", func() {
			page.showDuplicateDialog()
		}),
		widget.NewButton("Rename", func() {
			page.showRenameDialog()
		}),
		widget.NewButton("Delete", func() {
			page.deletePreset()
		}),
		widget.NewButton("Export Config", func() {
			page.exportPreset()
		}),
		widget.NewButton("Import Config", func() {
			page.importPreset()
		}),
	)

	editorCard := sectionCard(
		"Preset Editor",
		"Manage reusable dataset workflows for preview, import, dry-run validation, and export.",
		container.NewVBox(form, widget.NewSeparator(), actions),
	)

	detailsCard := sectionCard(
		"Preset Detail",
		"Review the currently selected preset in a read-friendly summary.",
		page.detailLabel,
	)

	activityCard := sectionCard(
		"Recent Workflow Activity",
		"See the latest preset, import, export, and maintenance operations from the same workspace.",
		page.activityLabel,
	)

	page.root = standardScroll(container.NewVBox(editorCard, container.NewGridWithColumns(2, detailsCard, activityCard)))
	page.Refresh()
	return page
}

// CanvasObject returns the preset-library UI for embedding in the shell.
func (p *PresetManagerPage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads preset options and recent activity.
func (p *PresetManagerPage) Refresh() {
	presets, err := p.application.ListDatasetPresets()
	if err != nil {
		p.detailLabel.SetText("Unable to load presets: " + err.Error())
		return
	}

	options := make([]string, 0, len(presets))
	for _, preset := range presets {
		options = append(options, preset.Name)
	}
	p.presetSelect.Options = options
	p.presetSelect.Refresh()

	if len(options) == 0 {
		p.presetSelect.ClearSelected()
		p.detailLabel.SetText("No saved presets yet.")
	} else if p.presetSelect.Selected == "" || !containsOption(options, p.presetSelect.Selected) {
		p.presetSelect.SetSelected(options[0])
	} else {
		_ = p.loadSelectedPreset()
	}

	activity, err := p.application.RecentActivity(10)
	if err != nil {
		p.activityLabel.SetText("Unable to load recent activity: " + err.Error())
		return
	}

	if len(activity) == 0 {
		p.activityLabel.SetText("No recent activity yet.")
		return
	}

	lines := make([]string, 0, len(activity))
	for _, entry := range activity {
		lines = append(lines, fmt.Sprintf("%s | %s | %s", entry.Timestamp.Local().Format("2006-01-02 15:04"), entry.Action, entry.Detail))
	}
	p.activityLabel.SetText(strings.Join(lines, "\n"))
}

func (p *PresetManagerPage) loadSelectedPreset() error {
	name := strings.TrimSpace(p.presetSelect.Selected)
	if name == "" {
		return nil
	}

	preset, err := p.application.FindDatasetPreset(name)
	if err != nil {
		return err
	}

	p.nameEntry.SetText(preset.Name)
	p.collectionEntry.SetText(preset.Collection)
	p.keyFieldEntry.SetText(preset.KeyField)
	p.conflictSelect.SetSelected(preset.ConflictMode)
	p.queryEntry.SetText(preset.QueryText)
	p.detailLabel.SetText(formatPresetSummary(preset))
	return nil
}

func (p *PresetManagerPage) savePreset() error {
	preset := studioapp.DatasetPreset{
		Name:         strings.TrimSpace(p.nameEntry.Text),
		Collection:   strings.TrimSpace(p.collectionEntry.Text),
		KeyField:     strings.TrimSpace(p.keyFieldEntry.Text),
		ConflictMode: strings.TrimSpace(strings.ToLower(p.conflictSelect.Selected)),
		QueryText:    strings.TrimSpace(p.queryEntry.Text),
	}
	if err := p.application.SaveDatasetPreset(preset); err != nil {
		return err
	}
	return nil
}

func (p *PresetManagerPage) deletePreset() {
	name := strings.TrimSpace(p.nameEntry.Text)
	if name == "" {
		dialog.ShowInformation("No Preset", "Select or enter a preset name first.", p.window)
		return
	}

	dialog.ShowConfirm("Delete Preset", fmt.Sprintf("Delete preset %q?", name), func(confirmed bool) {
		if !confirmed {
			return
		}
		if err := p.application.DeleteDatasetPreset(name); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.nameEntry.SetText("")
		p.queryEntry.SetText("")
		p.Refresh()
		p.onStatusChanged()
	}, p.window)
}

func (p *PresetManagerPage) showRenameDialog() {
	oldName := strings.TrimSpace(p.nameEntry.Text)
	if oldName == "" {
		dialog.ShowInformation("No Preset", "Select a preset first.", p.window)
		return
	}

	newNameEntry := widget.NewEntry()
	newNameEntry.SetText(oldName)
	form := sectionCard("Rename Preset", "Change the durable preset name without losing its workflow fields.", newNameEntry)
	confirm := dialog.NewCustomConfirm("Rename Preset", "Rename", "Cancel", form, func(confirmed bool) {
		if !confirmed {
			return
		}
		if err := p.application.RenameDatasetPreset(oldName, newNameEntry.Text); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.Refresh()
		p.nameEntry.SetText(strings.TrimSpace(newNameEntry.Text))
		p.onStatusChanged()
	}, p.window)
	confirm.Resize(fyne.NewSize(480, 220))
	confirm.Show()
}

func (p *PresetManagerPage) showDuplicateDialog() {
	sourceName := strings.TrimSpace(p.nameEntry.Text)
	if sourceName == "" {
		dialog.ShowInformation("No Preset", "Select a preset first.", p.window)
		return
	}

	newNameEntry := widget.NewEntry()
	newNameEntry.SetPlaceHolder("Copy name")
	form := sectionCard("Duplicate Preset", "Create a second reusable workflow from the current preset.", newNameEntry)
	confirm := dialog.NewCustomConfirm("Duplicate Preset", "Duplicate", "Cancel", form, func(confirmed bool) {
		if !confirmed {
			return
		}
		if err := p.application.DuplicateDatasetPreset(sourceName, newNameEntry.Text); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.Refresh()
		p.onStatusChanged()
	}, p.window)
	confirm.Resize(fyne.NewSize(480, 220))
	confirm.Show()
}

func (p *PresetManagerPage) exportPreset() {
	name := strings.TrimSpace(p.nameEntry.Text)
	if name == "" {
		dialog.ShowInformation("No Preset", "Select a preset first.", p.window)
		return
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		if writer == nil {
			return
		}

		path := writer.URI().Path()
		writer.Close()
		if _, err := p.application.ExportDatasetPresetConfig(name, path); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.Refresh()
		p.onStatusChanged()
	}, p.window)
	saveDialog.SetFileName(strings.ReplaceAll(name, " ", "-") + ".json")
	saveDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".json"}))
	saveDialog.Show()
}

func (p *PresetManagerPage) importPreset() {
	openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		if reader == nil {
			return
		}

		path := reader.URI().Path()
		reader.Close()
		if _, err := p.application.ImportDatasetPresetConfig(path); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.Refresh()
		p.onStatusChanged()
	}, p.window)
	openDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".json"}))
	openDialog.Show()
}

func formatPresetSummary(preset studioapp.DatasetPreset) string {
	lines := []string{
		"Name: " + preset.Name,
		"Collection: " + preset.Collection,
		"Key Field: " + preset.KeyField,
		"Conflict Mode: " + preset.ConflictMode,
	}
	if strings.TrimSpace(preset.QueryText) == "" {
		lines = append(lines, "Query: (none)")
	} else {
		lines = append(lines, "Query: "+preset.QueryText)
	}
	return strings.Join(lines, "\n")
}

func containsOption(options []string, target string) bool {
	for _, option := range options {
		if option == target {
			return true
		}
	}
	return false
}
