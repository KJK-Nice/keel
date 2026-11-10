package native

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// Dark-theme values of docs/design/tokens.json. TestTokensMatchJSON keeps them in step.
// shortcut: hand-copied; generate from tokens.json with internal/design/tokensgen when the list grows (docs/native.md).
const (
	hexChrome        = "#0c1117"
	hexStage         = "#080b10"
	hexSurface       = "#11161d"
	hexRaised        = "#1a2027"
	hexHover         = "#222831"
	hexLine          = "#2a3037"
	hexLineStrong    = "#5c646e"
	hexInk           = "#e9edf2"
	hexInkMuted      = "#a7adb5"
	hexInkFaint      = "#878d95"
	hexBrass         = "#e8b960"
	hexBrassInk      = "#e8b960"
	hexBrassSoft     = "#e8b96024"
	hexTide          = "#5dd2bd"
	hexSignal        = "#fc984c"
	hexFlare         = "#f66d67"
	hexTermSelection = "#e8b96047"
	hexBrassStrong   = "#f4cb75"
	hexOnBrass       = "#291906"
	hexSignalSoft    = "#fc984c24"
	hexFlareSoft     = "#f66d6726"
	hexOnFlare       = "#200a08"
	hexHaze          = "#85b6e9"
	hexHazeSoft      = "#85b6e926"
	hexScrim         = "#020407b8"
	hexTideSoft      = "#5dd2bd24"
	hexShadowModal   = "#000000a6" // shadow-modal's drop shadow; its 1px ring is #ffffff0f
	hexShadowPop     = "#00000080" // shadow-pop's drop shadow (menus, tooltips, toasts); its 1px ring is #ffffff0a
	// focus = brass-ink, term-fg = ink, term-bg = stage, term-cursor = brass.
	hexFocus      = hexBrassInk
	hexTermFg     = hexInk
	hexTermBg     = hexStage
	hexTermCursor = hexBrass
)

// hexAnsi is ansi-0..ansi-15.
var hexAnsi = [16]string{
	"#1c222a", "#f26d6a", "#8fcf8a", "#e8b960", "#7aaee8", "#c79bd8", "#5dd2bd", "#c5cbd3",
	"#4f5761", "#ff8f88", "#aee3a6", "#f5d08a", "#a3c8f2", "#dcb8e8", "#8ce6d6", "#f2f5f8",
}

// Sizes, spacing and radii in px.
const (
	sizeTitlebar  = 38
	sizeTab       = 28
	sizePanebar   = 30
	sizeControl   = 28
	sizeControlSm = 24
	sizeControlLg = 32
	sizeRow       = 44
	sizeSidebar   = 252
	sizeRail      = 208 // size-rail: the Settings section rail
	// Not tokens: the macOS traffic lights' close button is 14 DIPs tall (AppKit); TrafficLightPosition
	// is the corner of that button, so y = (size-titlebar - 14) / 2 centres the lights in the title bar row.
	trafficLightSize = 14
	space0_5         = 2
	space1           = 4
	space1_5         = 6
	space2           = 8
	space3           = 12
	space4           = 16
	space6           = 24
	space8           = 32
	radiusXs         = 3
	radiusSm         = 5
	radiusMd         = 8
	radiusLg         = 12
	radiusFull       = 999
	uiFontSize       = 13   // type body
	captionSize      = 12   // type label / caption
	windowOpacity    = 0.66 // vibrancy-tint-default
	weightRegular    = 400
	weightMedium     = 500
	weightSemibold   = 600
	titleSize        = 15 // type title
	displaySize      = 28 // type display
	kbdSize          = 11 // type kbd
	monoFamily       = "JetBrains Mono"
	monoFallback     = "monospace"
	statusbarSize    = 24 // not a token: "about 24" in the plan
	focusRingWidth   = 3  // shadow-tile-focus
	focusRingAlpha   = 0.30
	dimAlpha         = 0.70 // unfocused terminals in a split tab
	iconSize         = 16
	iconSizeSm       = 12   // k-tile--sm svg, 0.923rem
	sizeTileSm       = 20   // k-tile--sm, 1.538rem
	tileTintAlpha    = 0.16 // k-tile: 16% of the host colour
	tabMaxWidth      = 200  // not a token: the widest a tab grows before its name is cut
	minTileWidth     = 240  // not a token: narrowest a split tile may be dragged to
	minTileHeight    = 120  // not a token: shortest a stacked tile may be dragged to
	roleChipHeight   = 20   // k-role, components.css
	roleChipSize     = 11   // k-role font size
	dialogWidth      = 420  // .modal width in keel.css
	kbdBox           = 18   // k-kbd min-width and height, components.css
	dotSize          = 7    // k-dot, components.css
	dotRing          = 3    // k-dot--live ring
	paletteWidth     = 560  // k-palette, components.css
	paletteSearch    = 46   // k-palette-search height
	paletteTop       = 0.16 // .cmd-palette top: 16vh
)

func tok(hex string) ui.Color { return ui.Hex(hex) }

var (
	chrome        = tok(hexChrome)
	stage         = tok(hexStage)
	surface       = tok(hexSurface)
	raised        = tok(hexRaised)
	hoverGround   = tok(hexHover)
	line          = tok(hexLine)
	lineStrong    = tok(hexLineStrong)
	ink           = tok(hexInk)
	inkMuted      = tok(hexInkMuted)
	inkFaint      = tok(hexInkFaint)
	brass         = tok(hexBrass)
	brassInk      = tok(hexBrassInk)
	brassSoft     = tok(hexBrassSoft)
	tide          = tok(hexTide)
	signal        = tok(hexSignal)
	flare         = tok(hexFlare)
	focus         = tok(hexFocus)
	termSelection = tok(hexTermSelection)
	brassStrong   = tok(hexBrassStrong)
	onBrass       = tok(hexOnBrass)
	signalSoft    = tok(hexSignalSoft)
	flareSoft     = tok(hexFlareSoft)
	onFlare       = tok(hexOnFlare)
	haze          = tok(hexHaze)
	hazeSoft      = tok(hexHazeSoft)
	scrim         = tok(hexScrim)
	tideSoft      = tok(hexTideSoft)
	shadowModal   = tok(hexShadowModal)
	shadowPop     = tok(hexShadowPop)
)

// hostTint is the colour of a host's label. keel serve stores it as var(--ansi-N), a reference to
// an ANSI token; anything else (a legacy value) falls back to ink-muted.
func hostTint(color string) ui.Color {
	var n int
	if _, err := fmt.Sscanf(color, "var(--ansi-%d)", &n); err == nil && n >= 0 && n < len(hexAnsi) {
		return tok(hexAnsi[n])
	}
	return inkMuted
}
