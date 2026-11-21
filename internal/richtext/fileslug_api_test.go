package richtext

// fileTypeIconOf is FileTypeIcon (fileslug.go). Tests reach it only
// through this file, so the red-evidence commit can stub it.
func fileTypeIconOf(ft, mode, subtype string) string { return FileTypeIcon(ft, mode, subtype) }

// skillRefs are files.info answers for a skill canvas (F11) and a skill
// set folder (F12).
func skillRefs() map[string]CanvasRef {
	return map[string]CanvasRef{
		"F11": {Title: "Plan", Status: "normal", Kind: "canvas", Filetype: "quip", IsSkill: true},
		"F12": {Name: "skills", Status: "normal", Filetype: "folder", FolderType: "skill_set"},
	}
}
