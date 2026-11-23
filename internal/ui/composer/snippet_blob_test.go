package composer

import "testing"

// Synthetic: the snippet dialog uploads a Blob typed as uploadSnippet types
// it (type: g && g.mimeType || "text/plain"): its type's SnippetFileTypes
// mime type, text/plain for "auto", "text" and a type the table doesn't
// have. Markdown's is CodeMirror's mode, yaml-frontmatter, as in Slack's
// table.
func TestSnippetBlobType(t *testing.T) {
	for typ, want := range map[string]string{
		"auto": "text/plain", "text": "text/plain", "javascript": "text/javascript", "go": "text/x-go",
		"python": "text/x-python", "typescript": "text/typescript", "markdown": "yaml-frontmatter",
		"cfm": "text/plain", "nosuchtype": "text/plain",
	} {
		if got := snippetBlobType(typ); got != want {
			t.Errorf("snippetBlobType(%q) = %q, want %q", typ, got, want)
		}
	}
	if f := snippetUpload("lint.go", "go", "package main\n"); f.Type != "text/x-go" || f.SnippetType != "go" || f.Name != "lint.go" {
		t.Errorf("snippetUpload = %+v, want a go snippet's Blob typed text/x-go", f)
	}
}
