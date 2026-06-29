package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showRecordEditorDialog displays the create/edit form used by the explorer page.
func showRecordEditorDialog(
	window fyne.Window,
	title string,
	initialCollection string,
	initialKey string,
	initialValueKind string,
	initialValue string,
	onSave func(collection, key, valueKind, value string) error,
) {
	collectionEntry := widget.NewEntry()
	collectionEntry.SetPlaceHolder("collection")
	collectionEntry.SetText(initialCollection)

	keyEntry := widget.NewEntry()
	keyEntry.SetPlaceHolder("record key")
	keyEntry.SetText(initialKey)

	valueKindSelect := widget.NewSelect([]string{"raw", "json"}, nil)
	if initialValueKind == "" {
		initialValueKind = "raw"
	}
	valueKindSelect.SetSelected(initialValueKind)

	valueEntry := widget.NewMultiLineEntry()
	valueEntry.SetPlaceHolder("record value")
	valueEntry.Wrapping = fyne.TextWrapWord
	valueEntry.SetMinRowsVisible(12)
	valueEntry.SetText(initialValue)

	form := sectionCard(
		title,
		"Create or update one record using either raw text or validated JSON.",
		container.NewBorder(
			container.NewVBox(
				widget.NewLabel("Collection"),
				collectionEntry,
				widget.NewSeparator(),
				widget.NewLabel("Key"),
				keyEntry,
				widget.NewSeparator(),
				widget.NewLabel("Value Kind"),
				valueKindSelect,
				widget.NewSeparator(),
				widget.NewLabel("Value"),
			),
			nil,
			nil,
			nil,
			valueEntry,
		),
	)

	editorDialog := dialog.NewCustomConfirm(title, "Save", "Cancel", standardScroll(form), func(confirmed bool) {
		if !confirmed {
			return
		}

		collection := strings.TrimSpace(collectionEntry.Text)
		key := strings.TrimSpace(keyEntry.Text)
		valueKind := strings.TrimSpace(valueKindSelect.Selected)
		if collection == "" {
			dialog.ShowError(fmt.Errorf("collection must not be empty"), window)
			return
		}
		if key == "" {
			dialog.ShowError(fmt.Errorf("key must not be empty"), window)
			return
		}

		if err := onSave(collection, key, valueKind, valueEntry.Text); err != nil {
			dialog.ShowError(err, window)
			return
		}
	}, window)

	editorDialog.Resize(fyne.NewSize(720, 560))
	editorDialog.Show()
}

// showDeleteConfirmation asks for confirmation before removing a record.
func showDeleteConfirmation(window fyne.Window, key string, onDelete func() error) {
	dialog.ShowConfirm("Delete Record", fmt.Sprintf("Delete %q?", key), func(confirmed bool) {
		if !confirmed {
			return
		}

		if err := onDelete(); err != nil {
			dialog.ShowError(err, window)
		}
	}, window)
}
