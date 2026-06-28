//go:build desktop

package main

import (
	"log"

	fyneapp "fyne.io/fyne/v2/app"

	studioapp "minidb-studio/internal/app"
	"minidb-studio/internal/ui"
)

// main opens the database-backed application and starts the native Fyne desktop window.
func main() {
	application, err := studioapp.NewApplication()
	if err != nil {
		log.Fatalf("start MiniDB Studio: %v", err)
	}

	fyneApplication := fyneapp.NewWithID("com.minidb.studio")
	mainWindow := ui.NewMainWindow(fyneApplication, application)
	mainWindow.ShowAndRun()
}
