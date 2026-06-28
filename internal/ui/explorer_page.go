package ui

import (
	"fmt"
	"strings"
	"time"

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
	window           fyne.Window
	application      *studioapp.Application
	onStatusChanged  func()
	root             fyne.CanvasObject
	collectionSelect *widget.Select
	filterEntry      *widget.Entry
	fieldEntry       *widget.Entry
	fieldValueEntry  *widget.Entry
	pageLabel        *widget.Label
	table            *widget.Table
	detailKey        *widget.Label
	detailMeta       *widget.Label
	detailValue      *widget.Entry
	editButton       *widget.Button
	deleteButton     *widget.Button
	records          []engine.Record
	selectedRow      int
	currentPage      int
	pageSize         int
	totalRecords     int
}

// NewExplorerPage constructs the record explorer UI and wires up all actions.
func NewExplorerPage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *ExplorerPage {
	page := &ExplorerPage{
		window:          window,
		application:     application,
		onStatusChanged: onStatusChanged,
		selectedRow:     -1,
		records:         make([]engine.Record, 0),
		pageSize:        100,
	}

	page.collectionSelect = widget.NewSelect([]string{engine.DefaultCollection}, nil)

	page.filterEntry = widget.NewEntry()
	page.filterEntry.SetPlaceHolder("Filter by key prefix")
	page.filterEntry.OnChanged = func(string) {
		page.currentPage = 0
		page.Refresh()
	}
	page.fieldEntry = widget.NewEntry()
	page.fieldEntry.SetPlaceHolder("JSON field")
	page.fieldEntry.OnChanged = func(string) {
		page.currentPage = 0
		page.Refresh()
	}
	page.fieldValueEntry = widget.NewEntry()
	page.fieldValueEntry.SetPlaceHolder("JSON field value")
	page.fieldValueEntry.OnChanged = func(string) {
		page.currentPage = 0
		page.Refresh()
	}
	page.pageLabel = widget.NewLabel("")
	page.collectionSelect.OnChanged = func(string) {
		page.currentPage = 0
		page.Refresh()
	}

	refreshButton := widget.NewButton("Refresh", func() {
		page.Refresh()
		application.State().SetLastResult("Explorer refreshed")
		onStatusChanged()
	})

	createButton := widget.NewButton("Create Record", func() {
		showRecordEditorDialog(window, "Create Record", selectedOrDefault(page.collectionSelect.Selected), "", engine.ValueKindRaw, "", func(collection, key, valueKind, value string) error {
			if err := application.SaveRecord(collection, key, value, valueKind); err != nil {
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

		initialValue, _ := application.GetRecord(record.Collection, record.Key)
		showRecordEditorDialog(window, "Edit Record", record.Collection, record.Key, record.ValueKind, initialValue, func(collection, key, valueKind, value string) error {
			if err := application.RenameRecord(record.Collection, record.Key, collection, key, value, valueKind); err != nil {
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
			if err := application.DeleteRecord(record.Collection, record.Key); err != nil {
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
			return len(page.records), 4
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Wrapping = fyne.TextWrapOff
			return label
		},
		func(id widget.TableCellID, object fyne.CanvasObject) {
			label := object.(*widget.Label)
			record := page.records[id.Row]
			switch id.Col {
			case 0:
				label.SetText(record.Key)
			case 1:
				label.SetText(record.ValuePreview)
			case 2:
				label.SetText(fmt.Sprintf("%d", record.ValueSize))
			case 3:
				label.SetText(formatRecordTime(record.UpdatedAt))
			}
		},
	)
	page.table.SetColumnWidth(0, 240)
	page.table.SetColumnWidth(1, 360)
	page.table.SetColumnWidth(2, 90)
	page.table.SetColumnWidth(3, 170)
	page.table.OnSelected = func(id widget.TableCellID) {
		page.selectedRow = id.Row
		page.refreshDetails()
	}

	tableHeader := container.NewGridWithColumns(
		4,
		widget.NewLabelWithStyle("Key", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Value Preview", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Size", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("Updated", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)

	toolbar := container.NewHBox(
		widget.NewLabel("Collection"),
		page.collectionSelect,
		page.filterEntry,
		page.fieldEntry,
		page.fieldValueEntry,
		page.pageLabel,
		layout.NewSpacer(),
		widget.NewButton("Previous", func() {
			if page.currentPage > 0 {
				page.currentPage--
				page.Refresh()
			}
		}),
		widget.NewButton("Next", func() {
			maxPage := 0
			if page.totalRecords > 0 {
				maxPage = (page.totalRecords - 1) / page.pageSize
			}
			if page.currentPage < maxPage {
				page.currentPage++
				page.Refresh()
			}
		}),
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
	page.detailMeta = widget.NewLabel("")
	page.detailValue = widget.NewMultiLineEntry()
	page.detailValue.Disable()
	page.detailValue.Wrapping = fyne.TextWrapWord
	page.detailValue.SetMinRowsVisible(14)

	rightPanel := container.NewVBox(
		widget.NewLabelWithStyle("Record Details", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Selected Key"),
		page.detailKey,
		widget.NewLabel("Metadata"),
		page.detailMeta,
		widget.NewSeparator(),
		widget.NewLabel("Full Value"),
		page.detailValue,
	)

	split := container.NewHSplit(leftPanel, rightPanel)
	split.Offset = 0.68

	page.root = container.NewBorder(nil, nil, nil, nil, split)
	page.collectionSelect.SetSelected(engine.DefaultCollection)
	return page
}

// CanvasObject returns the root view for embedding in the main window.
func (p *ExplorerPage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads records from the database using the current prefix filter.
func (p *ExplorerPage) Refresh() {
	collections := p.application.ListCollections()
	p.collectionSelect.Options = collections
	if p.collectionSelect.Selected == "" || !containsString(collections, p.collectionSelect.Selected) {
		p.collectionSelect.SetSelected(selectedOrDefault(firstOrDefault(collections)))
	}

	collection := selectedOrDefault(p.collectionSelect.Selected)
	prefix := strings.TrimSpace(p.filterEntry.Text)
	field := strings.TrimSpace(p.fieldEntry.Text)
	fieldValue := p.fieldValueEntry.Text
	if field != "" && fieldValue != "" {
		p.records, p.totalRecords = p.application.ListRecordsByJSONField(collection, prefix, field, fieldValue, p.currentPage, p.pageSize)
	} else {
		p.records, p.totalRecords = p.application.ListRecords(collection, prefix, p.currentPage, p.pageSize)
	}

	if p.selectedRow >= len(p.records) {
		p.selectedRow = -1
	}

	start := 0
	if p.totalRecords > 0 {
		start = p.currentPage*p.pageSize + 1
	}
	end := p.currentPage*p.pageSize + len(p.records)
	p.pageLabel.SetText(fmt.Sprintf("Showing %d-%d of %d", start, end, p.totalRecords))
	p.table.Refresh()
	p.refreshDetails()
}

func (p *ExplorerPage) refreshDetails() {
	record, ok := p.selectedRecord()
	if !ok {
		p.detailKey.SetText("No record selected")
		p.detailMeta.SetText("")
		p.detailValue.SetText("")
		p.editButton.Disable()
		p.deleteButton.Disable()
		return
	}

	p.detailKey.SetText(record.Key)
	if metadata, ok := p.application.GetRecordMetadata(record.Collection, record.Key); ok {
		p.detailMeta.SetText(fmt.Sprintf(
			"Collection: %s\nKind: %s\nSize: %d bytes\nCreated: %s\nUpdated: %s\nSequence: %d",
			metadata.Collection,
			metadata.ValueKind,
			metadata.ValueSize,
			formatRecordTime(metadata.CreatedAt),
			formatRecordTime(metadata.UpdatedAt),
			metadata.LastSequence,
		))
	}
	fullValue, _ := p.application.GetRecord(record.Collection, record.Key)
	p.detailValue.Enable()
	p.detailValue.SetText(fullValue)
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
func formatRecordTime(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "Unknown"
	}

	return timestamp.Local().Format("2006-01-02 15:04:05")
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func firstOrDefault(items []string) string {
	if len(items) == 0 {
		return engine.DefaultCollection
	}
	return items[0]
}

func selectedOrDefault(value string) string {
	if strings.TrimSpace(value) == "" {
		return engine.DefaultCollection
	}
	return value
}
