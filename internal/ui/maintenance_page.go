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
	window             fyne.Window
	application        *studioapp.Application
	onStatusChanged    func()
	root               fyne.CanvasObject
	liveKeysValue      *widget.Label
	liveDataValue      *widget.Label
	activeSizeValue    *widget.Label
	segmentCountValue  *widget.Label
	snapshotCountValue *widget.Label
	replayCountValue   *widget.Label
	setOpsValue        *widget.Label
	deleteOpsValue     *widget.Label
	snapshotTimeValue  *widget.Label
	compactTimeValue   *widget.Label
}

// NewMaintenancePage builds the maintenance tools and stats display.
func NewMaintenancePage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *MaintenancePage {
	page := &MaintenancePage{
		window:             window,
		application:        application,
		onStatusChanged:    onStatusChanged,
		liveKeysValue:      widget.NewLabel(""),
		liveDataValue:      widget.NewLabel(""),
		activeSizeValue:    widget.NewLabel(""),
		segmentCountValue:  widget.NewLabel(""),
		snapshotCountValue: widget.NewLabel(""),
		replayCountValue:   widget.NewLabel(""),
		setOpsValue:        widget.NewLabel(""),
		deleteOpsValue:     widget.NewLabel(""),
		snapshotTimeValue:  widget.NewLabel(""),
		compactTimeValue:   widget.NewLabel(""),
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

	page.root = container.NewVBox(
		widget.NewLabelWithStyle("Database Statistics", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		statsGrid,
		widget.NewSeparator(),
		refreshButton,
		snapshotButton,
		validateButton,
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
