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
	page.commandInput.SetPlaceHolder("Examples:\nSET customer:1 Alice Smith\nSETIN users 42 Alice Smith\nSETJSON docs profile {\"profile\":{\"email\":\"ada@example.com\",\"score\":95},\"tags\":[\"admin\"],\"active\":true}\nFINDIN docs profile.email=ada@example.com active=true\nFINDIN docs active=true OR profile.score>=90\nFINDIN docs profile.bio~=local OR tags=admin\nCOLLECTIONS\nKEYSIN users user:\nSTATS")
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

	page.root = container.NewBorder(
		container.NewVBox(
			widget.NewLabelWithStyle("Command Input", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			page.commandInput,
			runButton,
			widget.NewSeparator(),
			widget.NewLabelWithStyle("Output and History", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		),
		nil,
		nil,
		nil,
		container.NewVScroll(page.historyOutput),
	)

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
