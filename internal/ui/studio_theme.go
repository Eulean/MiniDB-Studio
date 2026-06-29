package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// studioTheme gives MiniDB Studio a cleaner desktop look without changing native Fyne behavior.
type studioTheme struct{}

// NewStudioTheme returns the custom desktop theme used across the application shell.
func NewStudioTheme() fyne.Theme {
	return &studioTheme{}
}

func (t *studioTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 25, G: 118, B: 210, A: 255}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 76, G: 154, B: 255, A: 255}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 214, G: 232, B: 255, A: 255}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 210, G: 219, B: 230, A: 255}
	case theme.ColorNameHover:
		return color.NRGBA{R: 235, G: 243, B: 252, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 26, G: 36, B: 46, A: 255}
	case theme.ColorNameBackground:
		return color.NRGBA{R: 244, G: 247, B: 250, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 230, G: 237, B: 245, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 111, G: 127, B: 143, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 187, G: 197, B: 208, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 236, G: 240, B: 245, A: 255}
	}

	return theme.DefaultTheme().Color(name, variant)
}

func (t *studioTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (t *studioTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (t *studioTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 12
	case theme.SizeNameInlineIcon:
		return 18
	case theme.SizeNameScrollBar:
		return 14
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameInputBorder:
		return 1.5
	}

	return theme.DefaultTheme().Size(name)
}
