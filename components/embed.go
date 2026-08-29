package components

import "embed"

// Component JS only. Do not embed generated icon Go source into the binary.
//
//go:embed */*.js
var TemplFiles embed.FS
