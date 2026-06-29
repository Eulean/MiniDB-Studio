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

// QueryPage gives MiniDB Studio a dedicated read/query workspace for SQL-style exploration.
type QueryPage struct {
	window          fyne.Window
	application     *studioapp.Application
	onStatusChanged func()
	root            fyne.CanvasObject
	savedSelect     *widget.Select
	nameEntry       *widget.Entry
	notesEntry      *widget.Entry
	queryEntry      *widget.Entry
	resultEntry     *widget.Entry
}

// NewQueryPage builds the query studio page with saved queries, SQL execution, and result export.
func NewQueryPage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *QueryPage {
	page := &QueryPage{
		window:          window,
		application:     application,
		onStatusChanged: onStatusChanged,
		savedSelect:     widget.NewSelect([]string{}, nil),
		nameEntry:       widget.NewEntry(),
		notesEntry:      widget.NewMultiLineEntry(),
		queryEntry:      widget.NewMultiLineEntry(),
		resultEntry:     widget.NewMultiLineEntry(),
	}

	page.nameEntry.SetPlaceHolder("Saved query name")
	page.notesEntry.SetPlaceHolder("Notes about this query")
	page.notesEntry.SetMinRowsVisible(3)
	page.queryEntry.SetPlaceHolder("SELECT key, value_preview FROM docs WHERE active=true ORDER BY updated_at DESC LIMIT 25")
	page.queryEntry.SetMinRowsVisible(8)
	page.queryEntry.Wrapping = fyne.TextWrapWord
	page.resultEntry.Disable()
	page.resultEntry.SetMinRowsVisible(16)
	page.resultEntry.Wrapping = fyne.TextWrapOff
	page.resultEntry.TextStyle = fyne.TextStyle{Monospace: true}

	page.savedSelect.OnChanged = func(string) {
		if err := page.loadSelectedQuery(); err != nil {
			dialog.ShowError(err, page.window)
		}
	}

	runButton := widget.NewButton("Run Query", func() {
		if err := page.runQuery(); err != nil {
			dialog.ShowError(err, page.window)
		}
	})
	saveButton := widget.NewButton("Save Query", func() {
		if err := page.saveQuery(); err != nil {
			dialog.ShowError(err, page.window)
			return
		}
		page.Refresh()
		onStatusChanged()
	})
	renameButton := widget.NewButton("Rename Query", func() {
		page.renameQuery()
	})
	deleteButton := widget.NewButton("Delete Query", func() {
		page.deleteQuery()
	})
	exportButton := widget.NewButton("Export Result", func() {
		page.exportLastResult()
	})

	inputCard := sectionCard(
		"Query Studio",
		"Run saved or ad-hoc read-only SQL queries translated into MiniDB collection and JSON query operations.",
		container.NewVBox(
			widget.NewLabel("Saved Query"),
			page.savedSelect,
			widget.NewSeparator(),
			widget.NewLabel("Name"),
			page.nameEntry,
			widget.NewSeparator(),
			widget.NewLabel("Notes"),
			page.notesEntry,
			widget.NewSeparator(),
			widget.NewLabel("SQL Query"),
			page.queryEntry,
			widget.NewSeparator(),
			container.NewGridWithColumns(5, runButton, saveButton, renameButton, deleteButton, exportButton),
		),
	)

	resultCard := sectionCard(
		"Result Output",
		"Results are shown as tab-separated text and can be exported to `.tsv` for spreadsheet or review workflows.",
		container.NewVScroll(page.resultEntry),
	)

	page.root = standardScroll(container.NewVBox(inputCard, resultCard))
	page.Refresh()
	return page
}

// CanvasObject returns the query studio root view.
func (p *QueryPage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads saved query options.
func (p *QueryPage) Refresh() {
	queries, err := p.application.ListSavedQueries()
	if err != nil {
		p.resultEntry.Enable()
		p.resultEntry.SetText("Unable to load saved queries: " + err.Error())
		p.resultEntry.Disable()
		return
	}

	options := make([]string, 0, len(queries))
	for _, query := range queries {
		options = append(options, query.Name)
	}
	p.savedSelect.Options = options
	p.savedSelect.Refresh()

	if len(options) == 0 {
		p.savedSelect.ClearSelected()
		return
	}
	if p.savedSelect.Selected == "" || !containsOption(options, p.savedSelect.Selected) {
		p.savedSelect.SetSelected(options[0])
	}
}

func (p *QueryPage) loadSelectedQuery() error {
	name := strings.TrimSpace(p.savedSelect.Selected)
	if name == "" {
		return nil
	}

	query, err := p.application.FindSavedQuery(name)
	if err != nil {
		return err
	}

	p.nameEntry.SetText(query.Name)
	p.notesEntry.SetText(query.Notes)
	p.queryEntry.SetText(query.QueryText)
	return nil
}

func (p *QueryPage) runQuery() error {
	result, err := p.application.ExecuteCommand(p.queryEntry.Text)
	if err != nil {
		p.resultEntry.Enable()
		p.resultEntry.SetText("ERROR: " + err.Error())
		p.resultEntry.Disable()
		p.onStatusChanged()
		return err
	}

	p.resultEntry.Enable()
	p.resultEntry.SetText(result)
	p.resultEntry.Disable()
	p.onStatusChanged()
	return nil
}

func (p *QueryPage) saveQuery() error {
	query := studioapp.SavedQuery{
		Name:      strings.TrimSpace(p.nameEntry.Text),
		QueryText: strings.TrimSpace(p.queryEntry.Text),
		Notes:     strings.TrimSpace(p.notesEntry.Text),
	}
	return p.application.SaveSavedQuery(query)
}

func (p *QueryPage) renameQuery() {
	oldName := strings.TrimSpace(p.savedSelect.Selected)
	newName := strings.TrimSpace(p.nameEntry.Text)
	if oldName == "" {
		dialog.ShowInformation("No Query", "Select a saved query first.", p.window)
		return
	}
	if newName == "" {
		dialog.ShowInformation("Missing Name", "Enter the new saved query name.", p.window)
		return
	}
	if err := p.application.RenameSavedQuery(oldName, newName); err != nil {
		dialog.ShowError(err, p.window)
		return
	}
	p.Refresh()
	p.savedSelect.SetSelected(newName)
	p.onStatusChanged()
}

func (p *QueryPage) deleteQuery() {
	name := strings.TrimSpace(p.savedSelect.Selected)
	if name == "" {
		dialog.ShowInformation("No Query", "Select a saved query first.", p.window)
		return
	}

	dialog.ShowConfirm("Delete Query", fmt.Sprintf("Delete saved query %q?", name), func(confirmed bool) {
		if !confirmed {
			return
		}
		if err := p.application.DeleteSavedQuery(name); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.nameEntry.SetText("")
		p.notesEntry.SetText("")
		p.queryEntry.SetText("")
		p.resultEntry.Enable()
		p.resultEntry.SetText("")
		p.resultEntry.Disable()
		p.Refresh()
		p.onStatusChanged()
	}, p.window)
}

func (p *QueryPage) exportLastResult() {
	if strings.TrimSpace(p.queryEntry.Text) == "" {
		dialog.ShowInformation("No Query", "Run or enter a SQL query first.", p.window)
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

		if err := p.application.ExportMiniSQLResult(p.queryEntry.Text, path); err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.onStatusChanged()
	}, p.window)
	saveDialog.SetFileName("minidb-query-result.tsv")
	saveDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".tsv"}))
	saveDialog.Show()
}
