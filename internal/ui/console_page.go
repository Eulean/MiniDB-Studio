package ui

import (
	studioapp "minidb-studio/internal/app"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// ConsolePage wraps the single-command console and history output.
type ConsolePage struct {
	window          fyne.Window
	application     *studioapp.Application
	onStatusChanged func()
	root            fyne.CanvasObject
	commandInput    *widget.Entry
	historyOutput   *widget.Entry
}

// NewConsolePage constructs the console UI using the engine command surface.
func NewConsolePage(window fyne.Window, application *studioapp.Application, onStatusChanged func()) *ConsolePage {
	page := &ConsolePage{
		window:          window,
		application:     application,
		onStatusChanged: onStatusChanged,
	}

	page.commandInput = widget.NewMultiLineEntry()
	page.commandInput.SetPlaceHolder("Examples:\nSET customer:1 Alice Smith\nSETIN users 42 Alice Smith\nSETJSON docs profile {\"profile\":{\"email\":\"ada@example.com\",\"score\":95},\"tags\":[\"admin\"],\"active\":true}\nFINDIN docs profile.email=ada@example.com active=true\nFINDIN docs active=true OR (profile.score>=90 tags=admin)\nFINDIN docs (tags=admin OR tags=reviewer) NOT archived=true\nSELECT * FROM docs LIMIT 10\nSELECT COUNT(*) FROM docs WHERE active=true\nSELECT value_kind, COUNT(*) FROM docs GROUP BY value_kind ORDER BY count DESC LIMIT 10\nSELECT key, value_preview FROM docs ORDER BY updated_at DESC LIMIT 25\nSAVEPRESET \"Docs Workflow\" docs id overwrite \"active=true\"\nLISTPRESETS\nSHOWPRESET \"Docs Workflow\"\nDUPLICATEPRESET \"Docs Workflow\" \"Docs Copy\"\nRENAMEDPRESET \"Docs Copy\" \"Docs Archive\"\nEXPORTPRESETCONFIG \"Docs Archive\" \"C:\\data\\docs-archive-preset.json\"\nIMPORTPRESETCONFIG \"C:\\data\\docs-archive-preset.json\"\nPREVIEWNDJSON docs \"C:\\data\\docs.ndjson\"\nPREVIEWPRESET \"Docs Archive\" \"C:\\data\\docs.ndjson\"\nIMPORTPRESET \"Docs Archive\" \"C:\\data\\docs.ndjson\" dry-run\nIMPORTNDJSON docs \"C:\\data\\docs.ndjson\" id overwrite\nEXPORTQUERY docs \"active=true\" \"C:\\data\\active-docs.jsonl\"\nEXPORTPRESET \"Docs Archive\" \"C:\\data\\active-docs.jsonl\"\nDELETEPRESET \"Docs Archive\"\nCOLLECTIONS\nKEYSIN users user:\nSTATS")
	page.commandInput.Wrapping = fyne.TextWrapWord
	page.commandInput.SetMinRowsVisible(8)

	runButton := widget.NewButton("Run", func() {
		if _, err := application.ExecuteCommand(page.commandInput.Text); err != nil {
			page.Refresh()
			onStatusChanged()
			dialog.ShowError(err, window)
			return
		}

		page.commandInput.SetText("")
		page.Refresh()
		onStatusChanged()
	})

	page.historyOutput = widget.NewMultiLineEntry()
	page.historyOutput.Disable()
	page.historyOutput.SetMinRowsVisible(18)
	page.historyOutput.Wrapping = fyne.TextWrapOff
	page.historyOutput.TextStyle = fyne.TextStyle{Monospace: true}

	inputCard := sectionCard(
		"Command Input",
		"Run one command at a time with readable, durable feedback for MiniDB operations.",
		container.NewVBox(
			page.commandInput,
			container.NewHBox(layoutSpacer(), runButton),
		),
	)

	outputCard := sectionCard(
		"Output And History",
		"Console history is kept in-app so recent commands and results stay easy to review.",
		container.NewVScroll(page.historyOutput),
	)

	page.root = standardScroll(container.NewBorder(inputCard, nil, nil, nil, outputCard))

	page.Refresh()
	return page
}

// CanvasObject returns the root console view for embedding in the main window.
func (p *ConsolePage) CanvasObject() fyne.CanvasObject {
	return p.root
}

// Refresh reloads the history panel from shared application state.
func (p *ConsolePage) Refresh() {
	p.historyOutput.Enable()
	p.historyOutput.SetText(p.application.CommandHistory())
	p.historyOutput.Disable()
}
