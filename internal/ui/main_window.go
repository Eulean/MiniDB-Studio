package ui

import (
	studioapp "minidb-studio/internal/app"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// MainWindow owns the native desktop shell, page navigation, and shared status bar.
type MainWindow struct {
	application       *studioapp.Application
	window            fyne.Window
	contentHost       *fyne.Container
	locationLabel     *widget.Label
	statusLabel       *widget.Label
	lastResultLabel   *widget.Label
	navigationButtons map[studioapp.Page]*widget.Button
	explorerPage      *ExplorerPage
	consolePage       *ConsolePage
	maintenancePage   *MaintenancePage
	aboutPage         fyne.CanvasObject
}

// NewMainWindow creates the full MiniDB Studio desktop shell.
func NewMainWindow(fyneApplication fyne.App, application *studioapp.Application) *MainWindow {
	window := fyneApplication.NewWindow("MiniDB Studio")
	window.Resize(fyne.NewSize(1220, 780))
	window.CenterOnScreen()

	mainWindow := &MainWindow{
		application:       application,
		window:            window,
		locationLabel:     widget.NewLabel(""),
		statusLabel:       widget.NewLabel(""),
		lastResultLabel:   widget.NewLabel(""),
		navigationButtons: make(map[studioapp.Page]*widget.Button),
	}

	mainWindow.explorerPage = NewExplorerPage(window, application, mainWindow.refreshStatusBar)
	mainWindow.consolePage = NewConsolePage(window, application, mainWindow.refreshStatusBar)
	mainWindow.maintenancePage = NewMaintenancePage(window, application, mainWindow.refreshStatusBar)
	mainWindow.aboutPage = buildAboutPage()
	mainWindow.contentHost = container.NewMax()

	sidebar := mainWindow.buildSidebar()
	statusBar := mainWindow.buildStatusBar()
	header := container.NewVBox(
		widget.NewLabelWithStyle("MiniDB Studio", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Windows-first native key-value database studio built in Go and Fyne."),
	)

	mainWindow.window.SetContent(
		container.NewBorder(
			header,
			statusBar,
			sidebar,
			nil,
			mainWindow.contentHost,
		),
	)

	mainWindow.window.SetCloseIntercept(func() {
		_ = application.Close()
		mainWindow.window.Close()
	})

	mainWindow.showPage(studioapp.PageExplorer)
	return mainWindow
}

// ShowAndRun starts the Fyne event loop.
func (m *MainWindow) ShowAndRun() {
	m.window.ShowAndRun()
}

func (m *MainWindow) buildSidebar() fyne.CanvasObject {
	makeNavButton := func(label string, icon fyne.Resource, page studioapp.Page) *widget.Button {
		button := widget.NewButtonWithIcon(label, icon, func() {
			m.showPage(page)
		})
		button.Alignment = widget.ButtonAlignLeading
		m.navigationButtons[page] = button
		return button
	}

	return container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		container.NewVBox(
			makeNavButton("Data Explorer", theme.StorageIcon(), studioapp.PageExplorer),
			makeNavButton("Command Console", theme.ComputerIcon(), studioapp.PageConsole),
			makeNavButton("Maintenance", theme.SettingsIcon(), studioapp.PageMaintenance),
			makeNavButton("About", theme.InfoIcon(), studioapp.PageAbout),
			layout.NewSpacer(),
		),
	)
}

func (m *MainWindow) buildStatusBar() fyne.CanvasObject {
	return container.NewGridWithColumns(
		3,
		m.locationLabel,
		m.statusLabel,
		m.lastResultLabel,
	)
}

func (m *MainWindow) refreshStatusBar() {
	snapshot := m.application.State().Snapshot()
	m.locationLabel.SetText("Database: " + snapshot.DatabaseLocation)
	m.statusLabel.SetText("Status: " + snapshot.CurrentStatus)
	m.lastResultLabel.SetText("Last Result: " + snapshot.LastResult)
}

func (m *MainWindow) showPage(page studioapp.Page) {
	m.application.State().SetPage(page)

	switch page {
	case studioapp.PageExplorer:
		m.explorerPage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.explorerPage.CanvasObject()}
	case studioapp.PageConsole:
		m.consolePage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.consolePage.CanvasObject()}
	case studioapp.PageMaintenance:
		m.maintenancePage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.maintenancePage.CanvasObject()}
	case studioapp.PageAbout:
		m.contentHost.Objects = []fyne.CanvasObject{m.aboutPage}
	}

	for navPage, button := range m.navigationButtons {
		if navPage == page {
			button.Disable()
			continue
		}
		button.Enable()
	}

	m.contentHost.Refresh()
	m.refreshStatusBar()
}

func buildAboutPage() fyne.CanvasObject {
	aboutText := widget.NewRichTextFromMarkdown(`
# MiniDB Studio

MiniDB Studio is a native desktop app built with Go and Fyne around a custom append-only key-value engine.

## Current Capabilities
- local single-process durable storage
- collections and paged browsing
- JSON-aware records
- nested JSON path lookup with multi-condition operators
- validation, export, repair, backup, and compaction
- legacy local-data migration and stale-lock recovery

## Supported Commands
- SET key value
- GET key
- DELETE key
- KEYS [optional-prefix]
- SETIN collection key value
- GETIN collection key
- DELETEIN collection key
- KEYSIN collection [prefix]
- SETJSON collection key json-value
- FINDIN collection path=value path>=value path~=value
- STATS
- COMPACT
- SNAPSHOT
- VALIDATE
- EXPORT collection
- REPAIR

## Desktop Smoke Test
1. Launch the app and let the Explorer load.
2. Create a record from Data Explorer.
3. Run a console command such as STATS or FINDIN docs profile.score>=90 tags=admin.
4. Open Maintenance and verify stats appear.
5. Close and relaunch to confirm persistence.

## Not Included Yet
- SQL
- networking
- authentication
- replication
- cloud sync
- multi-user access
`)

	return container.NewVScroll(aboutText)
}
