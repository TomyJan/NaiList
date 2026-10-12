package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// Files returns the frontend built before compiling the Go application.
func Files() fs.FS {
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
