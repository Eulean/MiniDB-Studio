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
	// MiniDB Studio intentionally uses one consistent light palette for now.
	// Forcing the light variant avoids dark-system fallback colors from making
	// dropdowns, popups, and overlays unreadable against our light custom shell.
	variant = theme.VariantLight

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
	case theme.ColorNameForegroundOnPrimary:
		return color.NRGBA{R: 245, G: 248, B: 252, A: 255}
	case theme.ColorNameBackground:
		return color.NRGBA{R: 236, G: 241, B: 247, A: 255}
	case theme.ColorNameButton:
		return color.NRGBA{R: 225, G: 233, B: 244, A: 255}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 210, G: 223, B: 239, A: 255}
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNameInputBorder:
		return color.NRGBA{R: 198, G: 210, B: 225, A: 255}
	case theme.ColorNameHeaderBackground:
		return color.NRGBA{R: 242, G: 246, B: 251, A: 255}
	case theme.ColorNameMenuBackground:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNameOverlayBackground:
		return color.NRGBA{R: 248, G: 250, B: 253, A: 255}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{R: 113, G: 125, B: 142, A: 255}
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 164, G: 176, B: 190, A: 255}
	case theme.ColorNameDisabledButton:
		return color.NRGBA{R: 232, G: 237, B: 243, A: 255}
	case theme.ColorNameScrollBar:
		return color.NRGBA{R: 175, G: 188, B: 204, A: 220}
	case theme.ColorNameScrollBarBackground:
		return color.NRGBA{R: 229, G: 235, B: 242, A: 255}
	case theme.ColorNameShadow:
		return color.NRGBA{R: 22, G: 34, B: 49, A: 48}
	}

	return theme.DefaultTheme().Color(name, theme.VariantLight)
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
