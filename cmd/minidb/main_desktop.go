//go:build desktop

package main

import (
	"log"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	studioapp "minidb-studio/internal/app"
	"minidb-studio/internal/ui"
)

// main opens the database-backed application and starts the native Fyne desktop window.
func main() {
	fyneApplication := fyneapp.NewWithID("com.minidb.studio")
	fyneApplication.Settings().SetTheme(ui.NewStudioTheme())

	application, err := studioapp.NewApplication()
	if err != nil {
		log.Printf("start MiniDB Studio: %v", err)

		errorWindow := fyneApplication.NewWindow("MiniDB Studio Startup Error")
		errorWindow.Resize(fyne.NewSize(760, 220))
		errorWindow.SetContent(container.NewVBox(
			widget.NewLabelWithStyle("MiniDB Studio could not open the local database.", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			widget.NewLabel("The application started, but the local database could not be opened."),
			widget.NewSeparator(),
			widget.NewLabel(err.Error()),
		))
		errorWindow.ShowAndRun()
		return
	}

	mainWindow := ui.NewMainWindow(fyneApplication, application)
	mainWindow.ShowAndRun()
}
