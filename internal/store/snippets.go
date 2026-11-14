package store

import (
	"sync"

	"github.com/some-du6e/clack/internal/richtext"
)

// Snippets' expandables and fetched content (richtext.FileExpander), in
// memory only. Slack keeps a message file's expanded state in redux
// (expandedFiles, by file, channel, message and view: gMww's CJ/$J) and
// the content files.info returned in its file store
// (content_highlight_html, cut for display by computePreviewsForSnippet).

type snippetState struct {
	mu      sync.Mutex
	exp     map[string]uint8 // ch/ts/file -> snipExpanded | snipExpanding
	content map[string]snippetContent
	order   []string // content's file ids, oldest first (bounded)
}

type snippetContent struct {
	code      *richtext.CodeMirror
	truncated bool
}

const (
	snipExpanded uint8 = 1 << iota
	snipExpanding
)

// maxSnippetContents bounds the fetched contents kept (each at most
// 12,800 characters of code, as Slack keeps them).
const maxSnippetContents = 64

var snippetsOf sync.Map // *Store -> *snippetState

func (s *Store) snippets() *snippetState {
	if v, ok := snippetsOf.Load(s); ok {
		return v.(*snippetState)
	}
	v, _ := snippetsOf.LoadOrStore(s, &snippetState{exp: map[string]uint8{}, content: map[string]snippetContent{}})
	return v.(*snippetState)
}

func snippetKey(ch, ts, fileID string) string { return ch + "/" + ts + "/" + fileID }

// FileExpansion is richtext.FileExpander: file fileID's expandable in
// message ch/ts.
func (s *Store) FileExpansion(ch, ts, fileID string) (expanded, expanding bool) {
	st := s.snippets()
	st.mu.Lock()
	defer st.mu.Unlock()
	v := st.exp[snippetKey(ch, ts, fileID)]
	return v&snipExpanded != 0, v&snipExpanding != 0
}

// SetFileExpanded marks the file's expandable in ch/ts expanded or not
// (Slack's expandFile / collapseFile).
func (s *Store) SetFileExpanded(ch, ts, fileID string, on bool) {
	s.setSnippetBit(snippetKey(ch, ts, fileID), snipExpanded, on)
}

// SetFileExpanding marks the file's expandable in ch/ts as fetching
// (fetchAndExpand's expanding state) or not.
func (s *Store) SetFileExpanding(ch, ts, fileID string, on bool) {
	s.setSnippetBit(snippetKey(ch, ts, fileID), snipExpanding, on)
}

func (s *Store) setSnippetBit(k string, bit uint8, on bool) {
	st := s.snippets()
	st.mu.Lock()
	defer st.mu.Unlock()
	v := st.exp[k]
	if on {
		v |= bit
	} else {
		v &^= bit
	}
	if v == 0 {
		delete(st.exp, k)
	} else {
		st.exp[k] = v
	}
}

// SnippetContent is richtext.FileExpander: the highlighted content
// files.info brought for fileID, and whether it was cut for display.
func (s *Store) SnippetContent(fileID string) (*richtext.CodeMirror, bool, bool) {
	st := s.snippets()
	st.mu.Lock()
	defer st.mu.Unlock()
	c, ok := st.content[fileID]
	return c.code, c.truncated, ok
}

// PutSnippetContent records fileID's content (nil: files.info had none).
func (s *Store) PutSnippetContent(fileID string, code *richtext.CodeMirror, truncated bool) {
	st := s.snippets()
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.content[fileID]; !ok {
		st.order = append(st.order, fileID)
		for len(st.order) > maxSnippetContents {
			delete(st.content, st.order[0])
			st.order = st.order[1:]
		}
	}
	st.content[fileID] = snippetContent{code: code, truncated: truncated}
}
