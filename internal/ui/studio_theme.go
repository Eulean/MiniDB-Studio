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
		return color.NRGBA{R: 32, G: 101, B: 209, A: 255}
	case theme.ColorNameFocus:
		return color.NRGBA{R: 71, G: 131, B: 230, A: 255}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 220, G: 232, B: 248, A: 255}
	case theme.ColorNameSeparator:
		return color.NRGBA{R: 209, G: 219, B: 231, A: 255}
	case theme.ColorNameHover:
		return color.NRGBA{R: 236, G: 242, B: 249, A: 255}
	case theme.ColorNameForeground:
		return color.NRGBA{R: 34, G: 43, B: 58, A: 255}
	case theme.ColorNameBackground:
		return color.NRGBA{R: 236, G: 241, B: 247, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 225, G: 233, B: 244, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 113, G: 125, B: 142, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 164, G: 176, B: 190, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 232, G: 237, B: 243, A: 255}
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
