package richtext

// fileTypeIconOf is FileTypeIcon (fileslug.go). Tests reach it only
// through this file, so the red-evidence commit can stub it.
func fileTypeIconOf(ft, mode, subtype string) string { return FileTypeIcon(ft, mode, subtype) }
