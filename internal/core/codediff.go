package core

// The Code tab's patch model, ported from Slack's web client (build
// a84662d2d135): the patch renderer (module Afoo: parsePatchLines,
// hideWhitespaceChanges, buildSplitRows, getLineKey, the language map) and
// the Code tab's file list (lu3W: orderFiles, buildFileTree, compactNodes,
// getAutoCollapsedFiles). Pure functions; internal/ui draws them.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Patch line types.
const (
	PatchHunk    = "hunk"
	PatchAdded   = "added"
	PatchRemoved = "removed"
	PatchContext = "context"
)

// PatchLine is one parsed line (parsePatchLines): a hunk header, or a
// line with its old and new line numbers (0 = none).
type PatchLine struct {
	Type             string
	Content          string
	OldLine, NewLine int
}

// Key is getLineKey: "L:<old>" for a removed line, else "R:<new>".
func (l PatchLine) Key() string {
	if l.Type == PatchRemoved {
		return "L:" + strconv.Itoa(l.OldLine)
	}
	return "R:" + strconv.Itoa(l.NewLine)
}

var hunkRe = regexp.MustCompile(`@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// ParsePatch is parsePatchLines: a unified diff's lines. "\ No newline"
// markers are dropped; a context line loses its leading space.
func ParsePatch(patch string) []PatchLine {
	if patch == "" {
		return nil
	}
	var out []PatchLine
	oldN, newN := 0, 0
	for _, s := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(s, "@@"):
			if m := hunkRe.FindStringSubmatch(s); m != nil {
				oldN, _ = strconv.Atoi(m[1])
				newN, _ = strconv.Atoi(m[2])
			}
			out = append(out, PatchLine{Type: PatchHunk, Content: s})
		case strings.HasPrefix(s, "+"):
			out = append(out, PatchLine{Type: PatchAdded, Content: s[1:], NewLine: newN})
			newN++
		case strings.HasPrefix(s, "-"):
			out = append(out, PatchLine{Type: PatchRemoved, Content: s[1:], OldLine: oldN})
			oldN++
		case strings.HasPrefix(s, `\`):
		default:
			out = append(out, PatchLine{Type: PatchContext, Content: strings.TrimPrefix(s, " "), OldLine: oldN, NewLine: newN})
			oldN++
			newN++
		}
	}
	return out
}

func runEnd(ls []PatchLine, i int, typ string) int {
	for i < len(ls) && ls[i].Type == typ {
		i++
	}
	return i
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return -1
		}
		return r
	}, s)
}

// HideWhitespace is hideWhitespaceChanges ("Hide whitespace"): in each
// run of removed lines followed by added lines, a removed/added pair
// that differs only in whitespace becomes one context line.
func HideWhitespace(ls []PatchLine) []PatchLine {
	var out []PatchLine
	for a := 0; a < len(ls); {
		if ls[a].Type != PatchRemoved && ls[a].Type != PatchAdded {
			out = append(out, ls[a])
			a++
			continue
		}
		n := runEnd(ls, a, PatchRemoved)
		i := runEnd(ls, n, PatchAdded)
		rem, add := ls[a:n], ls[n:i]
		a = i
		type pos struct {
			at   []int
			next int
		}
		idx := map[string]*pos{}
		for k, l := range add {
			s := stripSpace(l.Content)
			if p := idx[s]; p != nil {
				p.at = append(p.at, k)
			} else {
				idx[s] = &pos{at: []int{k}}
			}
		}
		l, c := 0, 0
		for k, r := range rem {
			p := idx[stripSpace(r.Content)]
			if p == nil {
				continue
			}
			for p.next < len(p.at) && p.at[p.next] < c {
				p.next++
			}
			if p.next == len(p.at) {
				continue
			}
			j := p.at[p.next]
			p.next++
			out = append(out, rem[l:k]...)
			out = append(out, add[c:j]...)
			out = append(out, PatchLine{Type: PatchContext, Content: add[j].Content, OldLine: r.OldLine, NewLine: add[j].NewLine})
			l, c = k+1, j+1
		}
		out = append(out, rem[l:]...)
		out = append(out, add[c:]...)
	}
	return out
}

// SplitRow is one row of the split layout (buildSplitRows): a hunk
// header, or a left (old) and right (new) line index into the lines (-1 =
// the empty side). A context line is on both sides.
type SplitRow struct {
	Hunk        bool
	Index       int // the hunk line
	Left, Right int
}

// SplitRows is buildSplitRows: removed lines pair with the added lines
// that follow them, row by row.
func SplitRows(ls []PatchLine) []SplitRow {
	var out []SplitRow
	for a := 0; a < len(ls); {
		switch ls[a].Type {
		case PatchHunk:
			out = append(out, SplitRow{Hunk: true, Index: a, Left: -1, Right: -1})
			a++
			continue
		case PatchContext:
			out = append(out, SplitRow{Left: a, Right: a})
			a++
			continue
		}
		i := a
		r := runEnd(ls, i, PatchRemoved)
		s := runEnd(ls, r, PatchAdded)
		o, l := r-i, s-r
		for e := 0; e < max(o, l); e++ {
			row := SplitRow{Left: -1, Right: -1}
			if e < o {
				row.Left = i + e
			}
			if e < l {
				row.Right = r + e
			}
			out = append(out, row)
		}
		a = s
	}
	return out
}

// PatchCap is the patch renderer's first page: 100 lines, then "Show N
// more lines".
const PatchCap = 100

// FileChangeKind is getFileChangeKind: added, removed, renamed, else
// modified.
func FileChangeKind(status string) string {
	switch status {
	case "added", "removed", "renamed":
		return status
	}
	return "modified"
}

// ---- the file list ----

// FileTreeRow is one row of the file tree (flattenFileTree): a folder
// (its compacted path, "a/b") or a file, at a depth.
type FileTreeRow struct {
	Folder bool
	Name   string // the label: a folder's compacted segments, or the base name
	Path   string // a folder's full path, or the file's filename
	Depth  int
	File   *CodeDiffFile
}

type treeNode struct {
	name, path string
	folder     bool
	kids       []*treeNode
	file       *CodeDiffFile
}

// FileTree is buildFileTree + compactNodes + flattenFileTree: folders
// before files, each sorted by name, single-child folder chains compacted
// into one row ("src/auth").
func FileTree(files []CodeDiffFile) []FileTreeRow {
	root := &treeNode{folder: true}
	for i := range files {
		f := &files[i]
		parts := strings.Split(f.Filename, "/")
		n := root
		for k, p := range parts {
			if k == len(parts)-1 {
				n.kids = append(n.kids, &treeNode{name: p, path: f.Filename, file: f})
				break
			}
			var next *treeNode
			for _, c := range n.kids {
				if c.folder && c.name == p {
					next = c
					break
				}
			}
			if next == nil {
				next = &treeNode{name: p, path: strings.Join(parts[:k+1], "/"), folder: true}
				n.kids = append(n.kids, next)
			}
			n = next
		}
	}
	var compact func(n *treeNode) *treeNode
	compact = func(n *treeNode) *treeNode {
		for n.folder && len(n.kids) == 1 && n.kids[0].folder {
			c := n.kids[0]
			n = &treeNode{name: n.name + "/" + c.name, path: c.path, folder: true, kids: c.kids}
		}
		for i, c := range n.kids {
			n.kids[i] = compact(c)
		}
		sort.SliceStable(n.kids, func(i, j int) bool {
			a, b := n.kids[i], n.kids[j]
			if a.folder != b.folder {
				return a.folder
			}
			return a.name < b.name
		})
		return n
	}
	for i, c := range root.kids {
		root.kids[i] = compact(c)
	}
	sort.SliceStable(root.kids, func(i, j int) bool {
		a, b := root.kids[i], root.kids[j]
		if a.folder != b.folder {
			return a.folder
		}
		return a.name < b.name
	})
	var out []FileTreeRow
	var walk func(ns []*treeNode, depth int)
	walk = func(ns []*treeNode, depth int) {
		for _, n := range ns {
			out = append(out, FileTreeRow{Folder: n.folder, Name: n.name, Path: n.path, Depth: depth, File: n.file})
			if n.folder {
				walk(n.kids, depth+1)
			}
		}
	}
	walk(root.kids, 0)
	return out
}

// OrderFiles is orderFiles: in the PR layout the file tree's order, else
// generated files last.
func OrderFiles(files []CodeDiffFile, tree bool) []CodeDiffFile {
	if tree {
		var out []CodeDiffFile
		for _, r := range FileTree(files) {
			if r.File != nil {
				out = append(out, *r.File)
			}
		}
		return out
	}
	var out, gen []CodeDiffFile
	for _, f := range files {
		if f.IsGenerated {
			gen = append(gen, f)
		} else {
			out = append(out, f)
		}
	}
	return append(out, gen...)
}

// AutoCollapsed is getAutoCollapsedFiles: generated files and files with
// over 500 changed lines start collapsed, and so does everything once 15
// files or 500 rendered lines (each file counts at most its first 100)
// are open. commented lists files with anchored comments: those open
// anyway (up to 5 when they'd be over the caps).
func AutoCollapsed(files []CodeDiffFile, commented map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		out[f.Filename] = true
	}
	oversized := func(f CodeDiffFile) bool { return f.Additions+f.Deletions > 500 }
	n, lines, capped := 0, 0, false
	for _, f := range files {
		if f.IsGenerated || oversized(f) {
			continue
		}
		t := min(f.Additions+f.Deletions, PatchCap)
		if n >= 15 || lines+t > 500 {
			capped = true
			break
		}
		delete(out, f.Filename)
		n++
		lines += t
	}
	byName := map[string]CodeDiffFile{}
	for _, f := range files {
		byName[f.Filename] = f
	}
	var extra []string
	for _, f := range files {
		if commented[f.Filename] && out[f.Filename] {
			if bf, ok := byName[f.Filename]; !capped && ok && !oversized(bf) {
				delete(out, f.Filename)
			} else {
				extra = append(extra, f.Filename)
			}
		}
	}
	for i, name := range extra {
		if i >= 5 {
			break
		}
		delete(out, name)
	}
	return out
}

// codeLangs is the patch renderer's extension → shiki language map (Afoo R).
var codeLangs = map[string]string{
	"js": "javascript", "mjs": "javascript", "cjs": "javascript", "ts": "typescript", "tsx": "tsx", "jsx": "jsx",
	"py": "python", "rb": "ruby", "go": "go", "rs": "rust", "java": "java", "kt": "kotlin", "swift": "swift",
	"c": "c", "h": "c", "cpp": "cpp", "cc": "cpp", "cs": "csharp", "php": "php", "hack": "php", "hh": "php",
	"css": "css", "less": "less", "scss": "scss", "html": "html", "xml": "xml", "json": "json", "yaml": "yaml",
	"yml": "yaml", "md": "markdown", "sql": "sql", "sh": "shellscript", "bash": "shellscript", "zsh": "shellscript",
	"dockerfile": "dockerfile", "toml": "toml", "lua": "lua", "r": "r", "scala": "scala", "dart": "dart", "vue": "vue",
	"graphql": "graphql", "gql": "graphql", "proto": "protobuf", "elm": "elm", "ex": "elixir", "exs": "elixir",
	"erl": "erlang", "hs": "haskell", "clj": "clojure",
}

// CodeLang is getLangFromFilename: the file extension's language, "" for
// none (no highlighting).
func CodeLang(filename string) string {
	i := strings.LastIndexByte(filename, '.')
	ext := filename
	if i >= 0 {
		ext = filename[i+1:]
	}
	if j := strings.LastIndexByte(ext, '/'); j >= 0 {
		ext = ext[j+1:]
	}
	return codeLangs[strings.ToLower(ext)]
}
