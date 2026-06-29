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
	pageLabel         *widget.Label
	locationLabel     *widget.Label
	statusLabel       *widget.Label
	lastResultLabel   *widget.Label
	navigationButtons map[studioapp.Page]*widget.Button
	homePage          *HomePage
	explorerPage      *ExplorerPage
	queryPage         *QueryPage
	consolePage       *ConsolePage
	maintenancePage   *MaintenancePage
	presetManagerPage *PresetManagerPage
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

	mainWindow.homePage = NewHomePage(application, mainWindow.refreshStatusBar)
	mainWindow.explorerPage = NewExplorerPage(window, application, mainWindow.refreshStatusBar)
	mainWindow.queryPage = NewQueryPage(window, application, mainWindow.refreshStatusBar)
	mainWindow.consolePage = NewConsolePage(window, application, mainWindow.refreshStatusBar)
	mainWindow.maintenancePage = NewMaintenancePage(window, application, mainWindow.refreshStatusBar)
	mainWindow.presetManagerPage = NewPresetManagerPage(window, application, mainWindow.refreshStatusBar)
	mainWindow.aboutPage = buildAboutPage()
	mainWindow.contentHost = container.NewMax()

	sidebar := mainWindow.buildSidebar()
	statusBar := mainWindow.buildStatusBar()
	header := heroBanner(
		"MiniDB Studio",
		"Windows-first native key-value database studio built in Go and Fyne.",
		container.NewHBox(
			panelSurface(
				container.NewHBox(
					widget.NewIcon(theme.ComputerIcon()),
					container.NewVBox(
						widget.NewLabelWithStyle("Active Page", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
						mainWindow.pageLabel,
					),
				),
				chromePanelFill,
				chromeHeroBorder,
			),
		),
	)

	mainWindow.pageLabel.TextStyle = fyne.TextStyle{Bold: true}

	mainWindow.window.SetContent(
		container.NewStack(
			canvasBg(),
			container.NewPadded(
				container.NewBorder(
					header,
					statusBar,
					sidebar,
					nil,
					container.NewPadded(mainWindow.contentHost),
				),
			),
		),
	)

	mainWindow.window.SetCloseIntercept(func() {
		_ = application.Close()
		mainWindow.window.Close()
	})

	mainWindow.showPage(studioapp.PageHome)
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
		makeNavButton("Overview", theme.HomeIcon(), studioapp.PageHome),
		makeNavButton("Data Explorer", theme.StorageIcon(), studioapp.PageExplorer),
		makeNavButton("Query Studio", theme.SearchIcon(), studioapp.PageQuery),
		makeNavButton("Command Console", theme.ComputerIcon(), studioapp.PageConsole),
		makeNavButton("Maintenance", theme.SettingsIcon(), studioapp.PageMaintenance),
		makeNavButton("Preset Library", theme.DocumentIcon(), studioapp.PagePresets),
		makeNavButton("About", theme.InfoIcon(), studioapp.PageAbout),
		layout.NewSpacer(),
	)

	sidebarCard := sidebarPanel("Workspace", "Local MiniDB desktop workspace", sidebarContent)
	sidebarCard.Resize(fyne.NewSize(240, 0))
	return container.NewGridWrap(fyne.NewSize(240, 720), sidebarCard)
}

func (m *MainWindow) buildStatusBar() fyne.CanvasObject {
	m.locationLabel.Wrapping = fyne.TextWrapOff
	m.statusLabel.Wrapping = fyne.TextWrapOff
	m.lastResultLabel.Wrapping = fyne.TextWrapOff

	return panelSurface(container.NewHBox(
		widget.NewIcon(theme.StorageIcon()),
		widget.NewLabelWithStyle("Database", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.locationLabel,
		layout.NewSpacer(),
		widget.NewIcon(theme.ConfirmIcon()),
		widget.NewLabelWithStyle("Status", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.statusLabel,
		layout.NewSpacer(),
		widget.NewIcon(theme.InfoIcon()),
		widget.NewLabelWithStyle("Last Result", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		m.lastResultLabel,
	), chromePanelFill, chromePanelBorder)
}

func (m *MainWindow) refreshStatusBar() {
	snapshot := m.application.State().Snapshot()
	m.pageLabel.SetText(string(snapshot.CurrentPage))
	m.locationLabel.SetText(compactPath(snapshot.DatabaseLocation, 52))
	m.statusLabel.SetText(snapshot.CurrentStatus)
	m.lastResultLabel.SetText(compactText(snapshot.LastResult, 44))
}

func (m *MainWindow) showPage(page studioapp.Page) {
	m.application.State().SetPage(page)

	switch page {
	case studioapp.PageHome:
		m.homePage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.homePage.CanvasObject()}
	case studioapp.PageExplorer:
		m.explorerPage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.explorerPage.CanvasObject()}
	case studioapp.PageQuery:
		m.queryPage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.queryPage.CanvasObject()}
	case studioapp.PageConsole:
		m.consolePage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.consolePage.CanvasObject()}
	case studioapp.PageMaintenance:
		m.maintenancePage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.maintenancePage.CanvasObject()}
	case studioapp.PagePresets:
		m.presetManagerPage.Refresh()
		m.contentHost.Objects = []fyne.CanvasObject{m.presetManagerPage.CanvasObject()}
	case studioapp.PageAbout:
		m.contentHost.Objects = []fyne.CanvasObject{m.aboutPage}
	}

	for navPage, button := range m.navigationButtons {
		if navPage == page {
			button.Importance = widget.HighImportance
			continue
		}
		button.Importance = widget.MediumImportance
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
			widget.NewRichTextFromMarkdown("- local single-process durable storage\n- collections and paged browsing\n- JSON-aware records and nested query paths\n- validation, export, repair, backup, compaction\n- preset-driven dataset workflows\n- saved read-only SQL queries with result export\n- grouped query summaries and collection schema inspection"),
		),
	)

	workflows := sectionCard(
		"Recommended Workflow",
		"Use the app like an operational desktop studio rather than a raw command shell only.",
		widget.NewRichTextFromMarkdown("1. Start in **Overview** to check workspace health, activity, and collection shape.\n2. Use **Data Explorer** to browse one collection and edit records safely.\n3. Use **Query Studio** for saved SQL snippets, `COUNT(*)`, grouped summaries, schema inspection, and TSV result export.\n4. Use **Command Console** for scripted operations, raw commands, and preset workflows.\n5. Use **Preset Library** to curate reusable dataset workflows.\n6. Use **Maintenance** for validation, compaction, snapshots, import, export, and repair."),
	)

	commandFamilies := sectionCard(
		"Command Families",
		"MiniDB stays intentionally compact, but the console already covers the full local workflow surface.",
		widget.NewRichTextFromMarkdown("- Core KV: `SET`, `GET`, `DELETE`, `KEYS`\n- Collections: `SETIN`, `GETIN`, `DELETEIN`, `KEYSIN`\n- Documents: `SETJSON`, `FINDIN`\n- Dataset flows: `PREVIEWNDJSON`, `IMPORTNDJSON`, `EXPORTQUERY`\n- Presets: `SAVEPRESET`, `LISTPRESETS`, `SHOWPRESET`, `DUPLICATEPRESET`, `RENAMEDPRESET`, `EXPORTPRESETCONFIG`, `IMPORTPRESETCONFIG`, `DELETEPRESET`\n- Read-only SQL: `SELECT COUNT(*) FROM docs WHERE active=true`, `SELECT value_kind, COUNT(*) FROM docs GROUP BY value_kind ORDER BY count DESC`, `SELECT key, value_preview FROM docs ORDER BY updated_at DESC LIMIT 25`\n- Maintenance: `STATS`, `SNAPSHOT`, `VALIDATE`, `COMPACT`, `EXPORT`, `REPAIR`"),
	)

	limitations := sectionCard(
		"Intentional v1-v9 Limits",
		"MiniDB Studio is becoming more capable, but it is still deliberately not a server database.",
		widget.NewRichTextFromMarkdown("- no write-capable SQL engine\n- no networking\n- no authentication\n- no replication\n- no cloud sync\n- no multi-user access"),
	)

	grid := container.NewGridWithColumns(2, workflows, commandFamilies)
	return standardScroll(container.NewVBox(overview, grid, limitations))
}

func compactPath(value string, max int) string {
	return compactText(value, max)
}

func compactText(value string, max int) string {
	if len(value) <= max || max < 8 {
		return value
	}

	front := max / 2
	back := max - front - 3
	return value[:front] + "..." + value[len(value)-back:]
}
