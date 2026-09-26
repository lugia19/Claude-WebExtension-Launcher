package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Dark theme, in Claude's colors.
var (
	background = rgb(0x262624)
	textColor  = rgb(0xF5F4EE)
	dimColor   = rgb(0x9B9A94)
	accent     = rgb(0xD97757)
	warnColor  = rgb(0xE0B340)
	errorColor = rgb(0xE5484D)
	trackColor = rgb(0x3A3936)
	white      = rgb(0xFFFFFF)
)

func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xFF}
}

// launcherTheme is Fyne's dark theme in the colors above.
type launcherTheme struct{ fyne.Theme }

func newLauncherTheme() launcherTheme { return launcherTheme{theme.DefaultTheme()} }

func (t launcherTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return background
	case theme.ColorNameForeground, theme.ColorNameForegroundOnPrimary, theme.ColorNameForegroundOnError:
		return textColor
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return accent
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return dimColor
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return trackColor
	case theme.ColorNameWarning:
		return warnColor
	case theme.ColorNameError:
		return errorColor
	}
	return t.Theme.Color(name, theme.VariantDark)
}

// overrideTheme changes a few of launcherTheme's colors, for part of the window.
type overrideTheme struct {
	launcherTheme
	colors map[fyne.ThemeColorName]color.Color
}

func (t overrideTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if c, ok := t.colors[name]; ok {
		return c
	}
	return t.launcherTheme.Color(name, v)
}

// primary shows obj (a high-importance button) in white with dark text: the screen's
// main action (the other buttons stay grey, and orange is kept for progress and
// checkboxes).
func primary(obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(obj, overrideTheme{newLauncherTheme(), map[fyne.ThemeColorName]color.Color{
		theme.ColorNamePrimary:             white,
		theme.ColorNameForegroundOnPrimary: background,
	}})
}

// noFocusRing hides obj's focus ring: a checkbox takes focus when clicked, and the
// ring would linger around it.
func noFocusRing(obj fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(obj, overrideTheme{newLauncherTheme(), map[fyne.ThemeColorName]color.Color{
		theme.ColorNameFocus: color.Transparent,
	}})
}
