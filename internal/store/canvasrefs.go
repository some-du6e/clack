package store

import (
	"encoding/json"
	"sync"

	"github.com/some-du6e/clack/internal/richtext"
)

// Canvas slugs' data (richtext.CanvasResolver): what files.info says
// about a linked canvas and the rich text of linked sections
// (quip.getCanvasBulkSectionIdsData), in memory only. Lookups that miss
// are queued for core's resolve loop, which fills them and emits "names".

type canvasRefs struct {
	mu      sync.Mutex
	refs    map[string]richtext.CanvasRef
	secs    map[string]json.RawMessage // file id + "_" + section id
	seen    map[string]bool            // asked for already
	pending []string                   // "f"+file id | "s"+file id+"_"+section id
}

var canvasRefsOf sync.Map // *Store -> *canvasRefs

func (s *Store) canvasRefs() *canvasRefs {
	if v, ok := canvasRefsOf.Load(s); ok {
		return v.(*canvasRefs)
	}
	v, _ := canvasRefsOf.LoadOrStore(s, &canvasRefs{refs: map[string]richtext.CanvasRef{},
		secs: map[string]json.RawMessage{}, seen: map[string]bool{}})
	return v.(*canvasRefs)
}

// CanvasRef is richtext.CanvasResolver's canvas lookup.
func (s *Store) CanvasRef(fileID string) (richtext.CanvasRef, bool) {
	c := s.canvasRefs()
	c.mu.Lock()
	r, ok := c.refs[fileID]
	c.mu.Unlock()
	if !ok {
		s.noteCanvas("f" + fileID)
	}
	return r, ok
}

// CanvasSection is richtext.CanvasResolver's section lookup.
func (s *Store) CanvasSection(fileID, sectionID string) (json.RawMessage, bool) {
	c := s.canvasRefs()
	key := fileID + "_" + sectionID
	c.mu.Lock()
	r, ok := c.secs[key]
	c.mu.Unlock()
	if !ok {
		s.noteCanvas("s" + key)
	}
	return r, ok
}

// PutCanvasRef records what files.info said about a canvas.
func (s *Store) PutCanvasRef(fileID string, r richtext.CanvasRef) {
	c := s.canvasRefs()
	c.mu.Lock()
	c.refs[fileID] = r
	c.mu.Unlock()
}

// PutCanvasSection records a section's rich_text block (nil: it has
// none, or it is gone).
func (s *Store) PutCanvasSection(fileID, sectionID string, raw json.RawMessage) {
	c := s.canvasRefs()
	c.mu.Lock()
	c.secs[fileID+"_"+sectionID] = raw
	c.mu.Unlock()
}

// ForgetCanvas lets a canvas (and its sections) be asked for again, after
// a failed lookup or an edit.
func (s *Store) ForgetCanvas(fileID string) {
	c := s.canvasRefs()
	c.mu.Lock()
	delete(c.seen, "f"+fileID)
	for k := range c.seen {
		if len(k) > len(fileID)+1 && k[0] == 's' && k[1:len(fileID)+1] == fileID && k[len(fileID)+1] == '_' {
			delete(c.seen, k)
		}
	}
	c.mu.Unlock()
}

func (s *Store) noteCanvas(key string) {
	c := s.canvasRefs()
	c.mu.Lock()
	if c.seen[key] {
		c.mu.Unlock()
		return
	}
	c.seen[key] = true
	c.pending = append(c.pending, key)
	c.mu.Unlock()
	s.MissingNotify() // makes the channel
	s.missMu.Lock()
	select {
	case s.missNotify <- struct{}{}:
	default:
	}
	s.missMu.Unlock()
}

// TakeMissingCanvases returns and clears the canvases and sections
// lookups missed (sections as file id and section id pairs).
func (s *Store) TakeMissingCanvases() (files []string, sections [][2]string) {
	c := s.canvasRefs()
	c.mu.Lock()
	keys := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, k := range keys {
		switch k[0] {
		case 'f':
			files = append(files, k[1:])
		case 's':
			for i := 1; i < len(k); i++ {
				if k[i] == '_' {
					sections = append(sections, [2]string{k[1:i], k[i+1:]})
					break
				}
			}
		}
	}
	return
}
