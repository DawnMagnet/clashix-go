//go:build ui

package ui

import (
	"embed"
	"io/fs"
)

const panelVersion = "metacubexd v1.273.1"

//go:embed all:dist
var assets embed.FS

func init() {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return
	}
	bundled = sub
	bundledVersion = panelVersion
}
