package messages

import (
	"context"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// introRec is the DM foreword's header data: Slack's IMForewordMemberEntity
// (boot-shared VHud, build 9f450b8ad3db) reads the member's header section
// (useUserProfileSectionsQuery, XpEt: users.profile.getSections) through
// getHeaderData (oGjw w_): the Name Pronunciation text, which
// PronounsAndNamePronunciation shows after the pronouns, and the Name
// Recording's file, whose NamePronunciationAudioPlayer follows the name
// and the presence. Both are asked once per list (the section, then the
// recording's files.info).
type introRec struct {
	mu    sync.Mutex
	user  string
	asked bool
	pron  string      // namePronunciationText
	file  *slack.File // the recording, once files.info answered
	btn   videoplayer.NameSoundButton
}

// introHeader is the DM partner user's header data: the pronunciation
// text ("" until the section answers, or when there is none) and the
// name recording with the window's players (nil until both are there).
func (s *listState) introHeader(h Host, user string) (pron string, rec *richtext.File, ctl *videoplayer.Controller) {
	app := h.Core()
	if user == "" || app == nil {
		return "", nil, nil
	}
	r := &s.irec
	r.mu.Lock()
	if r.user != user {
		r.user, r.asked, r.pron, r.file = user, false, "", nil
	}
	asked := r.asked
	r.asked = true
	pron, f := r.pron, r.file
	r.mu.Unlock()
	if !asked {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ps, err := app.ProfileSections(ctx, user)
			if err != nil {
				return
			}
			hd := core.HeaderOf(ps)
			text := ""
			if hd.Pronunciation != nil {
				text = hd.Pronunciation.Text
			}
			r.mu.Lock()
			if r.user == user {
				r.pron = text
			}
			r.mu.Unlock()
			h.Invalidate()
			if hd.Record == nil || hd.Record.FileID == "" {
				return
			}
			fl, err := app.ProfileRecordingFile(ctx, hd.Record.FileID)
			if err != nil {
				return
			}
			r.mu.Lock()
			if r.user == user {
				r.file = fl
			}
			r.mu.Unlock()
			h.Invalidate()
		}()
	}
	vh, ok := h.(interface {
		Videos() *videoplayer.Controller
	})
	if !ok {
		return pron, nil, nil
	}
	ctl = vh.Videos()
	rf := richtext.FileOf(f)
	if ctl == nil || !ctl.AudioAvailable() || rf == nil || rf.URL == "" {
		return pron, nil, nil
	}
	return pron, rf, ctl
}
