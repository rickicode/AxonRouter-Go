package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:build
var buildFS embed.FS

// GetBuildFS returns the embedded build filesystem
func GetBuildFS() fs.FS {
	fsys, err := fs.Sub(buildFS, "build")
	if err != nil {
		panic("failed to create sub filesystem: " + err.Error())
	}
	return fsys
}

// GetHandler returns an http.Handler that serves the embedded frontend
func GetHandler() http.Handler {
	fsys := GetBuildFS()
	return http.FileServer(http.FS(fsys))
}
