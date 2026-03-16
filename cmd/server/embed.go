package main

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var webDistRaw embed.FS

func webDistFS() (fs.FS, error) {
	return fs.Sub(webDistRaw, "web/dist")
}
