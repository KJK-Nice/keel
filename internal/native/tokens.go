package native

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// hexPalette is one theme's colours as hex: the values of docs/design/tokens.json, which TestTokensMatchJSON keeps them in
// step with. This file is the only place that names a hex value or a theme; views read the colour variables below.
// shortcut: hand-copied; generate from tokens.json with internal/design/tokensgen when the list grows (docs/native.md).
type hexPalette struct {
	chrome, stage, surface, raised, hover, line, lineStrong, ink, inkMuted, inkFaint     string
	brass, brassInk, brassSoft, brassStrong, onBrass                                     string
	tide, tideSoft, signal, signalSoft, flare, flareSoft, onFlare, haze, hazeSoft, scrim string
	focus, termFg, termBg, termCursor, termSelection                                     string
	shadowModal, shadowPop                                                               string // the drop shadows of shadow-modal and shadow-pop; their 1px rings are not drawn
	ansi                                                                                 [16]string
}

var darkHex = hexPalette{
	chrome:        "#0c1117",
	stage:         "#080b10",
	surface:       "#11161d",
	raised:        "#1a2027",
	hover:         "#222831",
	line:          "#2a3037",
	lineStrong:    "#5c646e",
	ink:           "#e9edf2",
	inkMuted:      "#a7adb5",
	inkFaint:      "#878d95",
	brass:         "#e8b960",
	brassInk:      "#e8b960",
	brassSoft:     "#e8b96024",
	tide:          "#5dd2bd",
	signal:        "#fc984c",
	flare:         "#f66d67",
	termSelection: "#e8b96047",
	brassStrong:   "#f4cb75",
	onBrass:       "#291906",
	signalSoft:    "#fc984c24",
	flareSoft:     "#f66d6726",
	onFlare:       "#200a08",
	haze:          "#85b6e9",
	hazeSoft:      "#85b6e926",
	scrim:         "#020407b8",
	tideSoft:      "#5dd2bd24",
	focus:         "#e8b960",
	termFg:        "#e9edf2",
	termBg:        "#080b10",
	termCursor:    "#e8b960",
	shadowModal:   "#000000a6", shadowPop: "#00000080",
	ansi: [16]string{"#1c222a", "#f26d6a", "#8fcf8a", "#e8b960", "#7aaee8", "#c79bd8", "#5dd2bd", "#c5cbd3", "#4f5761", "#ff8f88", "#aee3a6", "#f5d08a", "#a3c8f2", "#dcb8e8", "#8ce6d6", "#f2f5f8"},
}

var lightHex = hexPalette{
	chrome:        "#e6eaef",
	stage:         "#fafcfe",
	surface:       "#f1f4f7",
	raised:        "#ffffff",
	hover:         "#dfe3ea",
	line:          "#d4d8dd",
	lineStrong:    "#878d95",
	ink:           "#141a22",
	inkMuted:      "#545c65",
	inkFaint:      "#696f77",
	brass:         "#e7b551",
	brassInk:      "#82510d",
	brassSoft:     "#e7b55138",
	tide:          "#0e6b62",
	signal:        "#a54801",
	flare:         "#ba2524",
	termSelection: "#e7b55159",
	brassStrong:   "#d7a035",
	onBrass:       "#291906",
	signalSoft:    "#a548011f",
	flareSoft:     "#ba25241a",
	onFlare:       "#fcfcfc",
	haze:          "#2c5ca3",
	hazeSoft:      "#2c5ca31c",
	scrim:         "#141a2266",
	tideSoft:      "#0e6b621f",
	focus:         "#82510d",
	termFg:        "#141a22",
	termBg:        "#fafcfe",
	termCursor:    "#82510d",
	shadowModal:   "#141a2233", shadowPop: "#141a221f",
	ansi: [16]string{"#141a22", "#c22f2c", "#2f7a32", "#8a5a0c", "#2f62ac", "#8a3fa0", "#00736a", "#5a626b", "#6f7780", "#d9443f", "#3e8f40", "#a06e16", "#3f74c4", "#9c52b2", "#0d877c", "#9aa1a8"},
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
	termShareLight   = 0.3  // vibrancy-term-share, light: the share of the chrome's see-through the terminal body keeps (dark is 1)
)

func tok(hex string) ui.Color { return ui.Hex(hex) }

// The colours of the theme in use. useTheme sets them all, once per frame, so a view reads them as plain values and
// never names a theme.
var (
	chrome, stage, surface, raised, hoverGround, line, lineStrong, ink, inkMuted, inkFaint ui.Color
	brass, brassInk, brassSoft, tide, signal, flare, focus, termSelection, brassStrong     ui.Color
	onBrass, signalSoft, flareSoft, onFlare, haze, hazeSoft, scrim, tideSoft               ui.Color
	shadowModal, shadowPop, termFg, termBg, termCursor                                     ui.Color
	ansi                                                                                   [16]ui.Color
	darkTheme                                                                              bool
)

func init() { useTheme(true) }

// paletteOf is the hex palette of a theme.
func paletteOf(dark bool) *hexPalette {
	if dark {
		return &darkHex
	}
	return &lightHex
}

// useTheme makes the colour variables those of the dark or the light theme.
func useTheme(dark bool) {
	p := paletteOf(dark)
	darkTheme = dark
	chrome, stage, surface, raised, hoverGround = tok(p.chrome), tok(p.stage), tok(p.surface), tok(p.raised), tok(p.hover)
	line, lineStrong, ink, inkMuted, inkFaint = tok(p.line), tok(p.lineStrong), tok(p.ink), tok(p.inkMuted), tok(p.inkFaint)
	brass, brassInk, brassSoft, brassStrong, onBrass = tok(p.brass), tok(p.brassInk), tok(p.brassSoft), tok(p.brassStrong), tok(p.onBrass)
	tide, tideSoft, signal, signalSoft = tok(p.tide), tok(p.tideSoft), tok(p.signal), tok(p.signalSoft)
	flare, flareSoft, onFlare, haze, hazeSoft, scrim = tok(p.flare), tok(p.flareSoft), tok(p.onFlare), tok(p.haze), tok(p.hazeSoft), tok(p.scrim)
	focus, termFg, termBg, termCursor, termSelection = tok(p.focus), tok(p.termFg), tok(p.termBg), tok(p.termCursor), tok(p.termSelection)
	shadowModal, shadowPop = tok(p.shadowModal), tok(p.shadowPop)
	for i, h := range p.ansi {
		ansi[i] = tok(h)
	}
}

// hostTint is the colour of a host's label. keel serve stores it as var(--ansi-N), a reference to
// an ANSI token; anything else (a legacy value) falls back to ink-muted.
func hostTint(color string) ui.Color {
	var n int
	if _, err := fmt.Sscanf(color, "var(--ansi-%d)", &n); err == nil && n >= 0 && n < len(ansi) {
		return ansi[n]
	}
	return inkMuted
}
