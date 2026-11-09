package fakeslack

import (
	"net/url"
	"sort"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The "blockkit-gallery" fixture: a channel of synthetic Block Kit
// messages for the Block Kit tests and CI screenshots. Each area adds its
// messages from its own gallery file (bkgallery_<area>.go) with
// registerGallery, and its fake API with registerGalleryAPI; this file
// only collects them.

// BlockKitGalleryID is the gallery channel (#blockkit-gallery).
const BlockKitGalleryID = "C0BKGALLERY"

type galleryPart struct {
	name string
	msgs func(s *Server) []slack.Message // oldest first
}

var (
	galleryMu    sync.Mutex
	galleryParts []galleryPart
	galleryAPIs  []func(s *Server, method string, f url.Values) (any, bool)
	galleryConvs []func(s *Server) (slack.Conversation, func(*Server) []slack.Message)
)

// registerGalleryConv adds a conversation (an app DM…) and its history,
// newest first (init time).
func registerGalleryConv(c func(s *Server) (slack.Conversation, func(*Server) []slack.Message)) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	galleryConvs = append(galleryConvs, c)
}

// addGalleryConvs puts the gallery channel and the areas' conversations
// in the viewer's list (after the generated ones, so no fixture shifts).
func (s *Server) addGalleryConvs() {
	galleryMu.Lock()
	cs := append([]func(*Server) (slack.Conversation, func(*Server) []slack.Message)(nil), galleryConvs...)
	galleryMu.Unlock()
	s.convs = append(s.convs, galleryConv())
	for _, c := range cs {
		cv, _ := c(s)
		s.convs = append(s.convs, cv)
	}
}

// galleryHistoryOf is the history of a gallery conversation (ok false
// for the generated ones).
func (s *Server) galleryHistoryOf(ch string) ([]slack.Message, bool) {
	if ch == BlockKitGalleryID {
		return s.galleryHistory(), true
	}
	galleryMu.Lock()
	cs := append([]func(*Server) (slack.Conversation, func(*Server) []slack.Message)(nil), galleryConvs...)
	galleryMu.Unlock()
	for _, c := range cs {
		if cv, h := c(s); cv.ID == ch {
			return h(s), true
		}
	}
	return nil, false
}

// emit sends a realtime event: to Emit when a test set it (clients
// without the socket), else over the realtime socket.
func (s *Server) emit(ev map[string]any) {
	if f := s.Emit; f != nil {
		f(ev)
		return
	}
	s.Push(ev)
}

// registerGallery adds an area's messages (init time).
func registerGallery(name string, msgs func(s *Server) []slack.Message) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	galleryParts = append(galleryParts, galleryPart{name: name, msgs: msgs})
	sort.SliceStable(galleryParts, func(i, j int) bool { return galleryParts[i].name < galleryParts[j].name })
}

// registerGalleryAPI adds an area's fake methods (init time); the first
// to answer a method wins.
func registerGalleryAPI(h func(s *Server, method string, f url.Values) (any, bool)) {
	galleryMu.Lock()
	defer galleryMu.Unlock()
	galleryAPIs = append(galleryAPIs, h)
}

// Gallery is Small with the blockkit-gallery fixture.
var Gallery = func() Size { sz := Small; sz.Gallery = true; return sz }()

// galleryConv is the gallery channel as users.conversations lists it.
func galleryConv() slack.Conversation {
	return slack.Conversation{ID: BlockKitGalleryID, Name: "blockkit-gallery", IsChannel: true, IsMember: true,
		Created: 1762500000, NumMembers: 3, ContextTeamID: TeamID,
		Purpose: &slack.Purpose{Value: "Synthetic Block Kit fixtures"}}
}

// galleryHistory is the gallery's messages, newest first.
func (s *Server) galleryHistory() []slack.Message {
	galleryMu.Lock()
	parts := append([]galleryPart(nil), galleryParts...)
	galleryMu.Unlock()
	var all []slack.Message
	for _, p := range parts {
		all = append(all, p.msgs(s)...)
	}
	sort.SliceStable(all, func(i, j int) bool { return slackTSLess(all[j].TS, all[i].TS) })
	return all
}

// handleGallery answers the gallery's own methods and those of the
// areas' fake apps.
func (s *Server) handleGallery(method string, f url.Values) (any, bool) {
	galleryMu.Lock()
	apis := append([]func(*Server, string, url.Values) (any, bool)(nil), galleryAPIs...)
	galleryMu.Unlock()
	for _, h := range apis {
		if out, ok := h(s, method, f); ok {
			return out, true
		}
	}
	return nil, false
}
