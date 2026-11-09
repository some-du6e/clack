package fakeslack

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The Block Kit gallery: one channel, #blockkit-gallery, that every Block
// Kit fixture shares. Each area adds its messages from its own file with
// RegisterGallery in an init func (bkgallery_elements.go: interactive
// elements), so files never collide; parts are ordered by name, and a
// part's messages keep their order. Everything is synthetic.

// GalleryChannelID is #blockkit-gallery.
const GalleryChannelID = "C0BKGALLERY"

// GalleryBotID, GalleryAppID and GalleryBotUserID are the gallery app:
// its messages carry them (bot_id, bot_profile), so blocks.actions gets a
// service_id and app_id.
const (
	GalleryBotID     = "B0BKGALLRY"
	GalleryAppID     = "A0BKGALLRY"
	GalleryBotUserID = "U0BKGALLRY"
)

var (
	galleryMu    sync.Mutex
	galleryParts = map[string]func(s *Server) []slack.Message{}
)

// RegisterGallery adds part's messages (oldest first) to the gallery.
func RegisterGallery(part string, msgs func(s *Server) []slack.Message) {
	galleryMu.Lock()
	galleryParts[part] = msgs
	galleryMu.Unlock()
}

// galleryBase is the gallery's first message time (fixed: screenshots).
var galleryBase = time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC).Unix()

// GalleryMessages builds the gallery history, oldest first, with ts
// assigned in order (one minute apart) unless a message has its own.
func (s *Server) GalleryMessages() []slack.Message {
	galleryMu.Lock()
	names := make([]string, 0, len(galleryParts))
	for n := range galleryParts {
		names = append(names, n)
	}
	parts := galleryParts
	galleryMu.Unlock()
	sort.Strings(names)
	var out []slack.Message
	for _, n := range names {
		out = append(out, parts[n](s)...)
	}
	for i := range out {
		m := &out[i]
		if m.TS == "" {
			m.TS = fmt.Sprintf("%d.%06d", galleryBase+int64(i)*60, i+1)
		}
		if m.Type == "" {
			m.Type = "message"
		}
		if m.BotID == "" && m.User == "" {
			m.BotID = GalleryBotID
		}
		if m.BotID == GalleryBotID && m.BotProfile == nil {
			m.BotProfile = &slack.BotProfile{ID: GalleryBotID, Name: "Gallery Bot", AppID: GalleryAppID, UserID: GalleryBotUserID, TeamID: TeamID}
		}
	}
	return out
}

// addGallery puts #blockkit-gallery in the sidebar with its history
// (newest first, like every history here).
func (s *Server) addGallery() {
	msgs := s.GalleryMessages()
	if len(msgs) == 0 {
		return
	}
	newest := make([]slack.Message, len(msgs))
	for i := range msgs {
		newest[len(msgs)-1-i] = msgs[i]
	}
	s.convMu.Lock()
	s.convs = append(s.convs, slack.Conversation{ID: GalleryChannelID, Name: "blockkit-gallery", IsChannel: true, IsMember: true,
		Created: galleryBase - 86400, Creator: SelfID, NumMembers: 3, ContextTeamID: TeamID,
		Topic: &slack.Purpose{Value: "Every Block Kit block and element"}, Purpose: &slack.Purpose{Value: "Synthetic Block Kit fixtures"}})
	s.convMu.Unlock()
	s.histMu.Lock()
	s.hist[GalleryChannelID] = newest
	s.histMu.Unlock()
}
