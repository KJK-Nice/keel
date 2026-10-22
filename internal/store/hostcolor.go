package store

// HostColor is one swatch of the host label palette. Value is what a host
// stores in Color: a reference to an ANSI 9–14 token (docs/design/tokens.json),
// so the tile follows the theme instead of carrying a literal.
type HostColor struct {
	Name  string
	Value string
}

// HostColors is the host label palette, in picker order.
var HostColors = []HostColor{
	{"Green", "var(--ansi-10)"},
	{"Blue", "var(--ansi-12)"},
	{"Magenta", "var(--ansi-13)"},
	{"Yellow", "var(--ansi-11)"},
	{"Red", "var(--ansi-9)"},
	{"Cyan", "var(--ansi-14)"},
}

// DefaultHostColor is the swatch a new host starts with.
var DefaultHostColor = HostColors[0].Value

// legacyHostColors maps the hex swatches older Keel versions stored to the
// token that replaced each, by hue.
var legacyHostColors = map[string]string{
	"#5ee0b5": "var(--ansi-10)",
	"#6ea8ff": "var(--ansi-12)",
	"#c084fc": "var(--ansi-13)",
	"#f5c15d": "var(--ansi-11)",
	"#ff6b7a": "var(--ansi-9)",
	"#67e8f9": "var(--ansi-14)",
}

// hostColor returns c with an old hex swatch replaced by its token. Any
// other value is kept as it is.
func hostColor(c string) string {
	if v, ok := legacyHostColors[c]; ok {
		return v
	}
	return c
}
