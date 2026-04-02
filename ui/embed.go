// Package ui embeds template and static assets for single-binary deployment.
package ui

import "embed"

// Templates holds the ui/templates/*.html files.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds the ui/static/ tree (CSS, JS, images).
//
//go:embed static/*
var Static embed.FS
