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
	window           fyne.Window
	application      *studioapp.Application
	onStatusChanged  func()
	root             fyne.CanvasObject
	savedSelect      *widget.Select
	nameEntry        *widget.Entry
	notesEntry       *widget.Entry
	queryEntry       *widget.Entry
	resultGrid       *widget.TextGrid
	schemaCollection *widget.Select
	schemaGrid       *widget.TextGrid
}

// NewQueryPage builds the query studio page with saved queries, SQL execution, and result export.
func NewQueryPage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *QueryPage {
	page := &QueryPage{
		window:           window,
		application:      application,
		onStatusChanged:  onStatusChanged,
		savedSelect:      widget.NewSelect([]string{}, nil),
		nameEntry:        widget.NewEntry(),
		notesEntry:       widget.NewMultiLineEntry(),
		queryEntry:       widget.NewMultiLineEntry(),
		resultGrid:       widget.NewTextGrid(),
		schemaCollection: widget.NewSelect([]string{}, nil),
		schemaGrid:       widget.NewTextGrid(),
	}

	page.nameEntry.SetPlaceHolder("Saved query name")
	page.notesEntry.SetPlaceHolder("Notes about this query")
	page.notesEntry.SetMinRowsVisible(3)
	page.queryEntry.SetPlaceHolder("SELECT key, value_preview FROM docs WHERE active=true ORDER BY updated_at DESC LIMIT 25 OFFSET 0")
	page.queryEntry.SetMinRowsVisible(8)
	page.queryEntry.Wrapping = fyne.TextWrapWord
	page.resultGrid.ShowLineNumbers = false
	page.resultGrid.SetText("Run a query to see results here.")
	page.schemaGrid.ShowLineNumbers = false
	page.schemaGrid.SetText("Inspect a collection to see schema details here.")

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
	inspectSchemaButton := widget.NewButton("Inspect Schema", func() {
		if err := page.inspectSchema(); err != nil {
			dialog.ShowError(err, page.window)
		}
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
		container.NewVScroll(page.resultGrid),
	)

	schemaCard := sectionCard(
		"Collection Schema",
		"Inspect observed JSON field paths, document coverage, and sample values for one collection.",
		container.NewVBox(
			widget.NewLabel("Collection"),
			page.schemaCollection,
			inspectSchemaButton,
			widget.NewSeparator(),
			container.NewVScroll(page.schemaGrid),
		),
	)

	page.root = standardScroll(container.NewVBox(inputCard, resultCard, schemaCard))
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
		p.resultGrid.SetText("Unable to load saved queries: " + err.Error())
		return
	}

	options := make([]string, 0, len(queries))
	for _, query := range queries {
		options = append(options, query.Name)
	}
	p.savedSelect.Options = options
	p.savedSelect.Refresh()

	collections := p.application.ListCollections()
	p.schemaCollection.Options = collections
	p.schemaCollection.Refresh()
	if len(collections) > 0 && (p.schemaCollection.Selected == "" || !containsOption(collections, p.schemaCollection.Selected)) {
		p.schemaCollection.SetSelected(collections[0])
	}

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
		p.resultGrid.SetText("ERROR: " + err.Error())
		p.onStatusChanged()
		return err
	}

	p.resultGrid.SetText(result)
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
		p.resultGrid.SetText("Run a query to see results here.")
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

func (p *QueryPage) inspectSchema() error {
	collection := strings.TrimSpace(p.schemaCollection.Selected)
	if collection == "" {
		return fmt.Errorf("select a collection first")
	}

	summary, err := p.application.InspectCollectionSchema(collection)
	if err != nil {
		return err
	}

	lines := []string{
		fmt.Sprintf("Collection: %s", summary.Collection),
		fmt.Sprintf("JSON Records: %d", summary.JSONRecordCount),
		fmt.Sprintf("Observed Fields: %d", len(summary.Fields)),
	}
	if len(summary.Fields) == 0 {
		lines = append(lines, "No JSON field data observed.")
	} else {
		for _, field := range summary.Fields {
			samples := strings.Join(field.SampleValues, ", ")
			if samples == "" {
				samples = "(no sample values)"
			}
			lines = append(lines, fmt.Sprintf("%s | docs=%d | distinct=%d | sample=%s", field.Path, field.ObservedDocuments, field.DistinctValueCount, samples))
		}
	}

	p.schemaGrid.SetText(strings.Join(lines, "\n"))
	return nil
}
