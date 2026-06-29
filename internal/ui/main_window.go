package ui

import (
	"fmt"
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
	pageLabel         *widget.Label
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
	window.Resize(fyne.NewSize(1360, 860))
	window.SetMaster()
	window.CenterOnScreen()

	mainWindow := &MainWindow{
		application:       application,
		window:            window,
		pageLabel:         widget.NewLabel(""),
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
	header := sectionCard(
		"MiniDB Studio",
		"Windows-first native key-value database studio built in Go and Fyne.",
		container.NewBorder(
			nil,
			nil,
			nil,
			container.NewHBox(
				widget.NewIcon(theme.ComputerIcon()),
				container.NewVBox(
					widget.NewLabelWithStyle("Active Page", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					mainWindow.pageLabel,
				),
			),
			container.NewVBox(
				widget.NewLabelWithStyle("Workspace", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
				compactHint("Resizable native desktop shell with persistent local storage."),
			),
		),
	)

	mainWindow.window.SetContent(
		container.NewPadded(
			container.NewBorder(
				header,
				statusBar,
				sidebar,
				nil,
				container.NewPadded(mainWindow.contentHost),
			),
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
		button := sidebarNavButton(label, icon, func() {
			m.showPage(page)
		})
		m.navigationButtons[page] = button
		return button
	}

	sidebarContent := container.NewVBox(
		widget.NewLabelWithStyle("Navigate", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		compactHint("Browse data, run commands, and maintain your local MiniDB from one desktop workspace."),
		spacerLine(),
		makeNavButton("Data Explorer", theme.StorageIcon(), studioapp.PageExplorer),
		makeNavButton("Command Console", theme.ComputerIcon(), studioapp.PageConsole),
		makeNavButton("Maintenance", theme.SettingsIcon(), studioapp.PageMaintenance),
		makeNavButton("About", theme.InfoIcon(), studioapp.PageAbout),
		layout.NewSpacer(),
	)

	return container.NewGridWrap(
		fyne.NewSize(260, 0),
		sectionCard(
			"Workspace",
			fmt.Sprintf("Database root: %s", m.application.DataDir()),
			sidebarContent,
		),
	)
}

func (m *MainWindow) buildStatusBar() fyne.CanvasObject {
	return container.NewGridWithColumns(3,
		statusCard("Database", m.locationLabel, theme.StorageIcon()),
		statusCard("Status", m.statusLabel, theme.ConfirmIcon()),
		statusCard("Last Result", m.lastResultLabel, theme.InfoIcon()),
	)
}

func (m *MainWindow) refreshStatusBar() {
	snapshot := m.application.State().Snapshot()
	m.pageLabel.SetText(string(snapshot.CurrentPage))
	m.locationLabel.SetText(snapshot.DatabaseLocation)
	m.statusLabel.SetText(snapshot.CurrentStatus)
	m.lastResultLabel.SetText(snapshot.LastResult)
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
	overview := sectionCard(
		"MiniDB Studio",
		"Native desktop database tooling built in Go and Fyne around a custom append-only engine.",
		container.NewVBox(
			compactHint("Designed for local persistence, practical data exploration, and teachable architecture rather than server-style complexity."),
			widget.NewSeparator(),
			widget.NewLabel("What it already does well"),
			widget.NewRichTextFromMarkdown("- local single-process durable storage\n- collections and paged browsing\n- JSON-aware records and nested query paths\n- validation, export, repair, backup, compaction\n- preset-driven dataset workflows"),
		),
	)

	workflows := sectionCard(
		"Recommended Workflow",
		"Use the app like an operational desktop studio rather than a raw command shell only.",
		widget.NewRichTextFromMarkdown("1. Start in **Data Explorer** to browse one collection.\n2. Use **Command Console** for scripted operations and preset workflows.\n3. Use **Maintenance** for validation, compaction, snapshots, import, export, and repair.\n4. Save reusable dataset presets once and reuse them across preview/import/export jobs."),
	)

	commandFamilies := sectionCard(
		"Command Families",
		"MiniDB stays intentionally compact, but the console already covers the full local workflow surface.",
		widget.NewRichTextFromMarkdown("- Core KV: `SET`, `GET`, `DELETE`, `KEYS`\n- Collections: `SETIN`, `GETIN`, `DELETEIN`, `KEYSIN`\n- Documents: `SETJSON`, `FINDIN`\n- Dataset flows: `PREVIEWNDJSON`, `IMPORTNDJSON`, `EXPORTQUERY`\n- Presets: `SAVEPRESET`, `LISTPRESETS`, `SHOWPRESET`, `DUPLICATEPRESET`, `RENAMEDPRESET`, `EXPORTPRESETCONFIG`, `IMPORTPRESETCONFIG`, `DELETEPRESET`\n- Maintenance: `STATS`, `SNAPSHOT`, `VALIDATE`, `COMPACT`, `EXPORT`, `REPAIR`"),
	)

	limitations := sectionCard(
		"Intentional v1-v7 Limits",
		"MiniDB Studio is becoming more capable, but it is still deliberately not a server database.",
		widget.NewRichTextFromMarkdown("- no SQL\n- no networking\n- no authentication\n- no replication\n- no cloud sync\n- no multi-user access"),
	)

	grid := container.NewGridWithColumns(2, workflows, commandFamilies)
	return standardScroll(container.NewVBox(overview, grid, limitations))
}
