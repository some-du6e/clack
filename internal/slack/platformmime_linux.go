//go:build linux

package slack

import (
	"os"
	"time"
)

// systemXDG is the process's shared-mime-info database, as Chromium keeps
// one (GetFileMimeType's statics).
var systemXDG = &xdgMime{dirs: func() []string { return xdgDataDirs(os.Getenv) }, now: time.Now}

// osMimeType is platform_mime_util_linux.cc's
// GetPlatformMimeTypeFromExtension: the XDG database's type for ext.
func osMimeType(ext string) (string, bool) { return systemXDG.platformType(ext) }
