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
	initialKey string,
	initialValue string,
	onSave func(key, value string) error,
) {
	keyEntry := widget.NewEntry()
	keyEntry.SetPlaceHolder("record key")
	keyEntry.SetText(initialKey)

	valueEntry := widget.NewMultiLineEntry()
	valueEntry.SetPlaceHolder("record value")
	valueEntry.Wrapping = fyne.TextWrapWord
	valueEntry.SetMinRowsVisible(12)
	valueEntry.SetText(initialValue)

	form := container.NewBorder(
		container.NewVBox(
			widget.NewLabel("Key"),
			keyEntry,
			widget.NewSeparator(),
			widget.NewLabel("Value"),
		),
		nil,
		nil,
		nil,
		valueEntry,
	)

	editorDialog := dialog.NewCustomConfirm(title, "Save", "Cancel", form, func(confirmed bool) {
		if !confirmed {
			return
		}

		key := strings.TrimSpace(keyEntry.Text)
		if key == "" {
			dialog.ShowError(fmt.Errorf("key must not be empty"), window)
			return
		}

		if err := onSave(key, valueEntry.Text); err != nil {
			dialog.ShowError(err, window)
			return
		}
	}, window)

	editorDialog.Resize(fyne.NewSize(520, 420))
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
