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

// introRec is the DM foreword's name recording: Slack's
// IMForewordMemberEntity (boot-shared VHud, build 9f450b8ad3db) puts the
// member's NamePronunciationAudioPlayer after their name and presence
// when the header data has a recording (namePronunciationAudioFileId).
// Here the button follows the DM's title, the partner's name: their
// header section (users.profile.getSections) and the recording's file
// (files.info) are asked once per list.
type introRec struct {
	mu    sync.Mutex
	user  string
	asked bool
	file  *slack.File
	btn   videoplayer.NameSoundButton
}

// introRecording is the DM partner user's name recording and the
// window's players, nil until both are there.
func (s *listState) introRecording(h Host, user string) (*richtext.File, *videoplayer.Controller) {
	if user == "" {
		return nil, nil
	}
	vh, ok := h.(interface {
		Videos() *videoplayer.Controller
	})
	if !ok {
		return nil, nil
	}
	ctl := vh.Videos()
	app := h.Core()
	if ctl == nil || !ctl.AudioAvailable() || app == nil {
		return nil, nil
	}
	r := &s.irec
	r.mu.Lock()
	if r.user != user {
		r.user, r.asked, r.file = user, false, nil
	}
	asked := r.asked
	r.asked = true
	f := r.file
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
	rf := richtext.FileOf(f)
	if rf == nil || rf.URL == "" {
		return nil, nil
	}
	return rf, ctl
}
