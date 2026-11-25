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
	prev    map[string]canvasThumb     // file id -> a message card's preview (richtext canvasfile.go)
	want    map[string]int64           // file id -> the newest updated time a card asked for
	seen    map[string]bool            // asked for already
	pending []string                   // "f"+file id | "s"+file id+"_"+section id | "p"+file id
}

var canvasRefsOf sync.Map // *Store -> *canvasRefs

func (s *Store) canvasRefs() *canvasRefs {
	if v, ok := canvasRefsOf.Load(s); ok {
		return v.(*canvasRefs)
	}
	v, _ := canvasRefsOf.LoadOrStore(s, &canvasRefs{refs: map[string]richtext.CanvasRef{},
		secs: map[string]json.RawMessage{}, prev: map[string]canvasThumb{}, want: map[string]int64{}, seen: map[string]bool{}})
	if s.released.Load() { // Release ran meanwhile, or before
		canvasRefsOf.CompareAndDelete(s, v)
	}
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

// KnownCanvasRef is a file's CanvasRef if files.info already answered
// for it, without asking for it.
func (s *Store) KnownCanvasRef(fileID string) (richtext.CanvasRef, bool) {
	c := s.canvasRefs()
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.refs[fileID]
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

// canvasThumb is a canvas's Block Kit thumbnail (nil: it has no preview)
// as of the file's updated time when it was read.
type canvasThumb struct {
	thumb   json.RawMessage
	updated int64
}

// CanvasPreview is richtext.CanvasPreviewer: a canvas's Block Kit
// thumbnail as last read (nil: the canvas has none, or none was read), ok
// when it was read since the file's updated time. It asks for nothing: a
// card on screen asks with WantCanvasPreview.
func (s *Store) CanvasPreview(fileID string, updated int64) (json.RawMessage, bool) {
	c := s.canvasRefs()
	c.mu.Lock()
	t, ok := c.prev[fileID]
	c.mu.Unlock()
	if !ok {
		return nil, false
	}
	// one read before the file's last edit: still the card's preview
	// until the new one comes, but not current (the card asks again)
	return t.thumb, t.updated >= updated
}

// WantCanvasPreview queues a canvas's thumbnail for core's resolve loop:
// a message card drew without one read since the file's updated time. It
// asks once per updated time (until ForgetCanvasPreview).
func (s *Store) WantCanvasPreview(fileID string, updated int64) {
	c := s.canvasRefs()
	c.mu.Lock()
	if t, ok := c.prev[fileID]; ok && t.updated >= updated {
		c.mu.Unlock()
		return
	}
	if c.want[fileID] < updated {
		c.want[fileID] = updated
		delete(c.seen, "p"+fileID) // a newer file than the last asked for
	}
	c.mu.Unlock()
	s.noteCanvas("p" + fileID)
}

// PutCanvasPreview records a canvas's Block Kit thumbnail (nil: none) as
// of the file's updated time, and as current for every time a card asked
// with (files.info may answer an older one than a message carries).
func (s *Store) PutCanvasPreview(fileID string, thumb json.RawMessage, updated int64) {
	c := s.canvasRefs()
	c.mu.Lock()
	c.prev[fileID] = canvasThumb{thumb: thumb, updated: max(updated, c.want[fileID])}
	c.mu.Unlock()
}

// ForgetCanvasPreview lets a canvas's card ask for its thumbnail again
// (after a failed read's backoff).
func (s *Store) ForgetCanvasPreview(fileID string) {
	c := s.canvasRefs()
	c.mu.Lock()
	delete(c.seen, "p"+fileID)
	c.mu.Unlock()
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
	delete(c.seen, "p"+fileID)
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

// TakeMissingCanvases returns and clears the canvases, sections and
// previews lookups missed (sections as file id and section id pairs).
func (s *Store) TakeMissingCanvases() (files []string, sections [][2]string, previews []string) {
	c := s.canvasRefs()
	c.mu.Lock()
	keys := c.pending
	c.pending = nil
	c.mu.Unlock()
	for _, k := range keys {
		switch k[0] {
		case 'f':
			files = append(files, k[1:])
		case 'p':
			previews = append(previews, k[1:])
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
