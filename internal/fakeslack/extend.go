package fakeslack

// Extension points for a curated workspace served on top of the fake
// (internal/demo: `clack demo`). The demo answers most methods itself
// through SetHook and SetEdgeHook, serves its own drawn images through
// SetImages, and puts its canvases, lists and scheduled messages into the
// fake's stores so the existing handlers (files.info, the canvas HTML
// export, lists.records.list, drafts.list…) serve them unchanged.

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

type extState struct {
	mu     sync.Mutex
	edge   func(method string, body map[string]json.RawMessage) (any, bool)
	images func(path string) ([]byte, string, bool)
}

var extStates sync.Map // *Server -> *extState

func (s *Server) ext() *extState {
	v, _ := extStates.LoadOrStore(s, &extState{})
	return v.(*extState)
}

// SetEdgeHook makes f see every edge API call (/api/edge/…, method
// "edge/<path>") with its JSON body, after SetHook's hook passed on it;
// ok=true answers it with out. nil removes it.
func (s *Server) SetEdgeHook(f func(method string, body map[string]json.RawMessage) (out any, ok bool)) {
	e := s.ext()
	e.mu.Lock()
	e.edge = f
	e.mu.Unlock()
}

// SetImages makes f serve /img/ requests first: path is what follows
// /img/; ok=false falls back to the generated gradient. nil removes it.
func (s *Server) SetImages(f func(path string) (b []byte, contentType string, ok bool)) {
	e := s.ext()
	e.mu.Lock()
	e.images = f
	e.mu.Unlock()
}

func (s *Server) edgeHook() func(string, map[string]json.RawMessage) (any, bool) {
	e := s.ext()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.edge
}

func (s *Server) imageHook() func(string) ([]byte, string, bool) {
	e := s.ext()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.images
}

// AddCanvas adds (or replaces) a canvas: files.info, files-pri's HTML
// export, canvases.edit and the rest then serve it. A canvas with a
// Channel is that channel's canvas tab (conversations.info). Zero
// Created/Updated become now; a nil Access an empty one.
func (s *Server) AddCanvas(c FakeCanvas) {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	now := time.Now().Unix()
	if c.Created == 0 {
		c.Created = now
	}
	if c.Updated == 0 {
		c.Updated = c.Created
	}
	if c.Access == nil {
		c.Access = map[string]string{}
	}
	if _, had := cs.byID[c.ID]; !had {
		cs.order = append(cs.order, c.ID)
	}
	cs.byID[c.ID] = &c
}

// CanvasFile is files.info's file object for canvas id (a message's
// files[] entry when the canvas is shared), or nil.
func (s *Server) CanvasFile(id string) map[string]any {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if c, ok := cs.byID[id]; ok {
		return s.canvasFile(c)
	}
	return nil
}

// AddList adds (or replaces) a list: files.info, lists.records.list,
// lists.cells.update and the rest then serve it.
func (s *Server) AddList(l FakeList) {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if _, had := ls.byID[l.ID]; !had {
		ls.order = append(ls.order, l.ID)
	}
	ls.byID[l.ID] = &l
}

// ListFile is files.info's file object for list id, or nil.
func (s *Server) ListFile(id string) map[string]any {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if l, ok := ls.byID[id]; ok {
		return s.listFile(l)
	}
	return nil
}

// AddDraft adds a server draft (drafts.list); DateScheduled > 0 makes it a
// scheduled message. Empty ID, ClientMsgID and LastUpdatedTS are filled in.
// It returns the draft's id.
func (s *Server) AddDraft(d Draft) string {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	if d.ID == "" {
		d.ID = s.newID("Dr")
	}
	if d.ClientMsgID == "" {
		d.ClientMsgID = d.ID + "-client"
	}
	if d.DateCreated == 0 {
		d.DateCreated = time.Now().Unix()
	}
	if d.LastUpdatedTS == "" {
		d.LastUpdatedTS = strconv.FormatInt(d.DateCreated*1000, 10)
	}
	s.drafts[d.ID] = &d
	return d.ID
}
