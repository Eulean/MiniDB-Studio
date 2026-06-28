package ui

import (
	"fmt"
	"strings"

	studioapp "minidb-studio/internal/app"
	"minidb-studio/internal/engine"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ExplorerPage owns the record browser, selection state, and editor actions.
type ExplorerPage struct {
	window          fyne.Window
	application     *studioapp.Application
	onStatusChanged func()
	root            fyne.CanvasObject
	filterEntry     *widget.Entry
	table           *widget.Table
	detailKey       *widget.Label
	detailValue     *widget.Entry
	editButton      *widget.Button
	deleteButton    *widget.Button
	records         []engine.Record
	selectedRow     int
}

// NewExplorerPage constructs the record explorer UI and wires up all actions.
func NewExplorerPage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *ExplorerPage {
	page := &ExplorerPage{
		window:          window,
		application:     application,
		onStatusChanged: onStatusChanged,
		selectedRow:     -1,
		records:         make([]engine.Record, 0),
	}

	page.filterEntry = widget.NewEntry()
	page.filterEntry.SetPlaceHolder("Filter by key prefix")
	page.filterEntry.OnChanged = func(string) {
		page.Refresh()
	}

	refreshButton := widget.NewButton("Refresh", func() {
		page.Refresh()
		application.State().SetLastResult("Explorer refreshed")
		onStatusChanged()
	})

	createButton := widget.NewButton("Create Record", func() {
		showRecordEditorDialog(window, "Create Record", "", "", func(key, value string) error {
			if err := application.SaveRecord(key, value); err != nil {
				return err
			}
			page.Refresh()
			onStatusChanged()
			return nil
		})
	})

	page.editButton = widget.NewButton("Edit Selected Record", func() {
		record, ok := page.selectedRecord()
		if !ok {
			dialog.ShowInformation("No Selection", "Select a record first.", window)
			return
		}

		showRecordEditorDialog(window, "Edit Record", record.Key, record.Value, func(key, value string) error {
			if err := application.RenameRecord(record.Key, key, value); err != nil {
				return err
			}
			page.Refresh()
			page.selectByKey(key)
			onStatusChanged()
			return nil
		})
	})

	page.deleteButton = widget.NewButton("Delete Selected Record", func() {
		record, ok := page.selectedRecord()
		if !ok {
			dialog.ShowInformation("No Selection", "Select a record first.", window)
			return
		}

		showDeleteConfirmation(window, record.Key, func() error {
			if err := application.DeleteRecord(record.Key); err != nil {
				return err
			}
			page.Refresh()
			onStatusChanged()
			return nil
		})
	})

	page.editButton.Disable()
	page.deleteButton.Disable()

	page.table = widget.NewTable(
		func() (int, int) {
			return len(page.records), 2
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextWrapOff
			return label
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			record := page.records[id.Row]
			if id.Col == 0 {
				label.SetText(record.Key)
				return
			}

			label.SetText(valuePreview(record.Value))
		},
	)
	page.table.SetColumnWidth(0, 240)
	page.table.SetColumnWidth(1, 420)
	page.table.OnSelected = func(id widget.TableCellID) {
		page.selectedRow = id.Row
		page.refreshDetails()
	}

	tableHeader := container.NewGridWithColumns(
		2,
		widget.NewLabelWithStyle("Key", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Value Preview", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	toolbar := container.NewHBox(
		page.filterEntry,
		layout.NewSpacer(),
		refreshButton,
		createButton,
		page.editButton,
		page.deleteButton,
	)

	leftPanel := container.NewBorder(
		container.NewVBox(toolbar, widget.NewSeparator(), tableHeader),
		nil,
		nil,
		nil,
		container.NewVScroll(page.table),
	)

	page.detailKey = widget.NewLabel("No record selected")
	page.detailValue = widget.NewMultiLineEntry()
	page.detailValue.Disable()
	page.detailValue.Wrapping = fyne.TextWrapWord
	page.detailValue.SetMinRowsVisible(14)

	rightPanel := container.NewVBox(
		widget.NewLabelWithStyle("Record Details", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Selected Key"),
		page.detailKey,
		widget.NewSeparator(),
		widget.NewLabel("Full Value"),
		page.detailValue,
	)

	split := container.NewHSplit(leftPanel, rightPanel)
	split.Offset = 0.68

	page.root = container.NewBorder(nil, nil, nil, nil, split)
	page.Refresh()
	return page
}

// CanvasObject returns the root view for embedding in the main window.
func (p *ExplorerPage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads records from the database using the current prefix filter.
func (p *ExplorerPage) Refresh() {
	p.records = p.application.ListRecords(strings.TrimSpace(p.filterEntry.Text))

	if p.selectedRow >= len(p.records) {
		p.selectedRow = -1
	}

	p.table.Refresh()
	p.refreshDetails()
}

func (p *ExplorerPage) refreshDetails() {
	record, ok := p.selectedRecord()
	if !ok {
		p.detailKey.SetText("No record selected")
		p.detailValue.SetText("")
		p.editButton.Disable()
		p.deleteButton.Disable()
		return
	}

	p.detailKey.SetText(record.Key)
	p.detailValue.Enable()
	p.detailValue.SetText(record.Value)
	p.detailValue.Disable()
	p.editButton.Enable()
	p.deleteButton.Enable()
}

func (p *ExplorerPage) selectedRecord() (engine.Record, bool) {
	if p.selectedRow < 0 || p.selectedRow >= len(p.records) {
		return engine.Record{}, false
	}

	return p.records[p.selectedRow], true
}

func (p *ExplorerPage) selectByKey(key string) {
	for index, record := range p.records {
		if record.Key == key {
			p.selectedRow = index
			p.refreshDetails()
			return
		}
	}

	p.selectedRow = -1
	p.refreshDetails()
}

func valuePreview(value string) string {
	singleLine := strings.ReplaceAll(value, "\r\n", "\n")
	singleLine = strings.ReplaceAll(singleLine, "\n", "\\n")
	if len(singleLine) <= 72 {
		return singleLine
	}

	return fmt.Sprintf("%s...", singleLine[:72])
}
