package ui

import (
	"fmt"
	"path/filepath"

	studioapp "minidb-studio/internal/app"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	fynestorage "fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
)

// MaintenancePage owns stats display and durable maintenance actions.
type MaintenancePage struct {
	window           fyne.Window
	application      *studioapp.Application
	onStatusChanged  func()
	root             fyne.CanvasObject
	liveKeysValue    *widget.Label
	logSizeValue     *widget.Label
	setOpsValue      *widget.Label
	deleteOpsValue   *widget.Label
	compactTimeValue *widget.Label
}

// NewMaintenancePage builds the maintenance tools and stats display.
func NewMaintenancePage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *MaintenancePage {
	page := &MaintenancePage{
		window:           window,
		application:      application,
		onStatusChanged:  onStatusChanged,
		liveKeysValue:    widget.NewLabel(""),
		logSizeValue:     widget.NewLabel(""),
		setOpsValue:      widget.NewLabel(""),
		deleteOpsValue:   widget.NewLabel(""),
		compactTimeValue: widget.NewLabel(""),
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

		saveDialog.SetFileName(filepath.Base(application.DataDir()) + "-backup.log")
		saveDialog.SetFilter(fynestorage.NewExtensionFileFilter([]string{".log", ".bak"}))
		saveDialog.Show()
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
		widget.NewLabelWithStyle("Log file size", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.logSizeValue,
		widget.NewLabelWithStyle("Total set operations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.setOpsValue,
		widget.NewLabelWithStyle("Total delete operations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.deleteOpsValue,
		widget.NewLabelWithStyle("Last compaction time", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), page.compactTimeValue,
	)

	page.root = container.NewVBox(
		widget.NewLabelWithStyle("Database Statistics", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		statsGrid,
		widget.NewSeparator(),
		refreshButton,
		compactButton,
		backupButton,
		openFolderButton,
	)

	page.Refresh()
	return page
}

// CanvasObject returns the root maintenance view for embedding in the main window.
func (p *MaintenancePage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads the database statistics labels.
func (p *MaintenancePage) Refresh() {
	stats, err := p.application.Stats()
	if err != nil {
		p.liveKeysValue.SetText("Unavailable")
		p.logSizeValue.SetText("Unavailable")
		p.setOpsValue.SetText("Unavailable")
		p.deleteOpsValue.SetText("Unavailable")
		p.compactTimeValue.SetText(err.Error())
		return
	}

	p.liveKeysValue.SetText(fmt.Sprintf("%d", stats.LiveKeyCount))
	p.logSizeValue.SetText(fmt.Sprintf("%d bytes", stats.LogFileSize))
	p.setOpsValue.SetText(fmt.Sprintf("%d", stats.TotalSetOperations))
	p.deleteOpsValue.SetText(fmt.Sprintf("%d", stats.TotalDeleteOps))
	if stats.LastCompactionTime.IsZero() {
		p.compactTimeValue.SetText("Never")
	} else {
		p.compactTimeValue.SetText(stats.LastCompactionTime.Local().Format("2006-01-02 15:04:05"))
	}
}
