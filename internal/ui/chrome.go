package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// sectionCard wraps related controls in one consistent visual container.
func sectionCard(title, subtitle string, content fyne.CanvasObject) fyne.CanvasObject {
	header := container.NewVBox(
		widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	if strings.TrimSpace(subtitle) != "" {
		header.Add(widget.NewLabel(subtitle))
	}

	return widget.NewCard("", "", container.NewPadded(container.NewBorder(header, nil, nil, nil, content)))
}

// statusCard renders one compact status value so the footer stays readable while resizing.
func statusCard(title string, value *widget.Label, icon fyne.Resource) fyne.CanvasObject {
	value.Wrapping = fyne.TextWrapWord
	return widget.NewCard("", "", container.NewHBox(
		widget.NewIcon(icon),
		container.NewVBox(
			widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			value,
		),
	))
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

// actionButtonRow groups page actions in a responsive-looking row.
func actionButtonRow(objects ...fyne.CanvasObject) fyne.CanvasObject {
	row := append([]fyne.CanvasObject{}, objects...)
	row = append(row, layoutSpacer())
	return container.NewHBox(row...)
}

func layoutSpacer() fyne.CanvasObject {
	return layout.NewSpacer()
}
