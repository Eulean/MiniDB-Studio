package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var (
	chromePanelFill     = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	chromePanelBorder   = color.NRGBA{R: 211, G: 220, B: 232, A: 255}
	chromeMutedText     = color.NRGBA{R: 93, G: 108, B: 124, A: 255}
	chromeHeroFill      = color.NRGBA{R: 20, G: 31, B: 48, A: 255}
	chromeHeroBorder    = color.NRGBA{R: 46, G: 87, B: 141, A: 255}
	chromeHeroText      = color.NRGBA{R: 244, G: 248, B: 252, A: 255}
	chromeSidebarFill   = color.NRGBA{R: 241, G: 246, B: 252, A: 255}
	chromeSidebarBorder = color.NRGBA{R: 211, G: 220, B: 232, A: 255}
	chromeAppBackground = color.NRGBA{R: 233, G: 239, B: 246, A: 255}
)

// sectionCard wraps related controls in one consistent visual container.
func sectionCard(title, subtitle string, content fyne.CanvasObject) fyne.CanvasObject {
	headerObjects := []fyne.CanvasObject{
		newTextLine(title, chromeMutedText, true),
	}
	if strings.TrimSpace(subtitle) != "" {
		headerObjects = append(headerObjects, newTextLine(subtitle, chromeMutedText, false))
	}

	body := container.NewBorder(
		container.NewVBox(headerObjects...),
		nil,
		nil,
		nil,
		content,
	)
	return panelSurface(body, chromePanelFill, chromePanelBorder)
}

// heroBanner renders a stronger application header with better product identity.
func heroBanner(title, subtitle string, trailing fyne.CanvasObject) fyne.CanvasObject {
	left := container.NewVBox(
		newTextLine(title, chromeHeroText, true),
		newTextLine(subtitle, chromeHeroText, false),
	)
	body := container.NewHBox(left, layout.NewSpacer(), trailing)
	return panelSurface(body, chromeHeroFill, chromeHeroBorder)
}

// sidebarPanel renders the navigation shell with slightly stronger separation.
func sidebarPanel(title, subtitle string, content fyne.CanvasObject) fyne.CanvasObject {
	body := container.NewBorder(
		container.NewVBox(
			newTextLine(title, chromeMutedText, true),
			newTextLine(subtitle, chromeMutedText, false),
		),
		nil,
		nil,
		nil,
		content,
	)
	return panelSurface(body, chromeSidebarFill, chromeSidebarBorder)
}

// sidebarNavButton keeps navigation buttons visually aligned and easy to scan.
func sidebarNavButton(label string, icon fyne.Resource, onTap func()) *widget.Button {
	button := widget.NewButtonWithIcon(label, icon, onTap)
	button.Alignment = widget.ButtonAlignLeading
	button.Importance = widget.MediumImportance
	return button
}

// spacerLine adds a small neutral divider between grouped sidebar sections.
func spacerLine() fyne.CanvasObject {
	return container.NewPadded(widget.NewSeparator())
}

// compactHint renders short helper copy in a quieter style.
func compactHint(text string) fyne.CanvasObject {
	label := widget.NewLabel(text)
	label.TextStyle = fyne.TextStyle{Italic: true}
	label.Wrapping = fyne.TextWrapWord
	return label
}

// standardScroll wraps content in a padded scroll container for consistent page spacing.
func standardScroll(content fyne.CanvasObject) fyne.CanvasObject {
	scroll := container.NewVScroll(container.NewPadded(content))
	scroll.SetMinSize(fyne.NewSize(0, 0))
	return scroll
}

func layoutSpacer() fyne.CanvasObject {
	return layout.NewSpacer()
}

func newTextLine(text string, fill color.Color, bold bool) fyne.CanvasObject {
	line := canvas.NewText(text, fill)
	line.TextSize = 14
	line.TextStyle = fyne.TextStyle{Bold: bold}
	if bold {
		line.TextSize = 15
	}
	return line
}

func panelSurface(content fyne.CanvasObject, fill color.Color, stroke color.Color) fyne.CanvasObject {
	background := canvas.NewRectangle(fill)
	background.StrokeColor = stroke
	background.StrokeWidth = 1
	return container.NewStack(background, container.NewPadded(content))
}

func canvasBg() fyne.CanvasObject {
	return canvas.NewRectangle(chromeAppBackground)
}
