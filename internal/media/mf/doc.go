// Package mf is Clack's video backend on Windows: Media Foundation's
// Media Engine (mf_windows.go, mf_windows.c). Importing it registers the
// backend with internal/media; on other systems, and on Windows builds
// without cgo (arm64), it registers nothing.
package mf
