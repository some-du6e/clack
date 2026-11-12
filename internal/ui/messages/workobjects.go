package messages

// Work object cards in message rows (richview/workobject.go): the
// experiments and live state richview reads each frame, and what the
// card's clicks do. Slack's ExternalWorkObjectEntity (boot-shared K3AG,
// build 4d0c78085232):
//
//	the card (aC): opens the item in the browser when the user's
//	  work_objects_click_opens_url_prefs says so (robR B), else the
//	  details flexpane (viewDetails: WorkObjectFlexpane on its Details tab)
//	the actions bar: Open (OpenExternalLinkButton: app_unfurl_url when
//	  the attachment has one, pFiZ B, else external_url), Preview /
//	  Details (the flexpane), Comments (its Comments tab), View
//	  conversations (its Conversations tab), More actions
//	  (WorkObjectContextMenu, popups.NewWorkObjectMenu)
//	the footer: Refresh (chat.refreshUnfurl, core.RefreshWorkObject) and
//	  the layout's primary actions (blocks.actions in the attachment's
//	  container)

import (
	"context"
	"encoding/json"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/uipref"
	"github.com/some-du6e/clack/internal/workobject"
)

// WorkObjectOpen is a work object the ui root opens in its flexpane.
type WorkObjectOpen struct {
	Entity       *workobject.Entity
	AppUnfurlURL string
	Channel, TS  string
	// Tab is the flexpane's tab: "details", "comments" or "threads".
	Tab string
}

// WorkObjectHost is optional: the ui root's work object flexpane
// (WorkObjectFlexpane) and Share dialog (ShareWorkObjectModal).
type WorkObjectHost interface {
	OpenWorkObject(o WorkObjectOpen)
	// ShareWorkObject opens the share dialog; false when the host has
	// none (the menu then leaves Share out).
	ShareWorkObject(o WorkObjectOpen) bool
}

// WorkObjectSharer is a WorkObjectHost that has the share dialog.
type WorkObjectSharer interface{ CanShareWorkObjects() bool }

// woStyleFlags are the experiments and prefs the cards read.
func woStyleFlags(h Host, app *core.App) richview.WorkObjectFlags {
	f := app.WorkObjectFlags()
	share := false
	if s, ok := h.(WorkObjectSharer); ok {
		share = s.CanShareWorkObjects()
	}
	return richview.WorkObjectFlags{
		Rearrange: f.Rearrange, RefreshTimestamp: f.RefreshTimestamp, CommentSync: f.CommentSync,
		EmbeddedPreview: f.EmbeddedPreview, CanShare: share, Locale: i18n.Locale(), Time24: uipref.Get().Time24,
		ClickPrefs: app.PrefString("work_objects_click_opens_url_prefs"), Gen: app.WorkObjectGen(),
	}
}

// woSource is core's work object state for richview.
type woSource struct{ app *core.App }

func (s woSource) WorkObjectState(ch, ts string, e *workobject.Entity) richview.WorkObjectState {
	latest := 0.0
	if e.Comments != nil {
		latest = workobject.Number(e.Comments.LatestTimestamp)
	}
	r, f, n := s.app.WorkObjectCardState(ch, ts, e.EntityID, latest)
	return richview.WorkObjectState{Refreshing: r, RefreshFailed: f, NewerComments: n}
}

// woOpenURL is OpenExternalLinkButton's link: the attachment's
// app_unfurl_url unless the app is the one whose unfurl URL isn't the
// item's (oyZ+ _d), else external_url.
func woOpenURL(wo *richtext.WorkObject) string {
	if wo.AppUnfurlURL != "" && wo.E.AppID != "A012G7WPK1Q" && wo.E.AppID != "A2RPP3NFR" {
		return wo.AppUnfurlURL
	}
	return richtext.SafeURL(wo.E.ExternalURL)
}

// WorkObject is a click on a work object card (richview.WorkObjectHandler).
func (r *richHandler) WorkObject(ev richview.WorkObjectEvent) {
	if ev.Att == nil || ev.Att.WorkObject == nil || ev.Att.WorkObject.E == nil {
		return
	}
	wo := ev.Att.WorkObject
	e := wo.E
	app := r.h.Core()
	ch, ts := ev.Channel, ev.TS
	if r.m != nil {
		ch, ts = r.m.Channel, r.m.TS
	}
	open := func(tab string) {
		if wh, ok := r.h.(WorkObjectHost); ok {
			wh.OpenWorkObject(WorkObjectOpen{Entity: e, AppUnfurlURL: wo.AppUnfurlURL, Channel: ch, TS: ts, Tab: tab})
		}
	}
	switch ev.Action {
	case richview.WOClick:
		if app != nil && richview.WorkObjectClickOpensURL(app.PrefString("work_objects_click_opens_url_prefs"), e) {
			if u := woOpenURL(wo); u != "" {
				openURL(u)
			}
			return
		}
		open("details")
	case richview.WOOpen:
		if u := woOpenURL(wo); u != "" {
			openURL(u)
		}
	case richview.WOPreview:
		open("details")
	case richview.WOComments:
		open("comments")
	case richview.WOConversations:
		open("threads")
	case richview.WOShare:
		if wh, ok := r.h.(WorkObjectHost); ok {
			wh.ShareWorkObject(WorkObjectOpen{Entity: e, AppUnfurlURL: wo.AppUnfurlURL, Channel: ch, TS: ts})
		}
	case richview.WORefresh:
		if app != nil {
			upd := 0.0
			if l := e.UnfurlLayout(); l != nil {
				upd = l.UpdatedAtUnix()
			}
			app.RefreshWorkObject(ch, ts, e.EntityID, upd)
		}
	case richview.WOButton:
		r.woButton(ev, ch, ts)
	case richview.WOMore:
		ph, ok := r.h.(popups.Host)
		if !ok {
			return
		}
		wh, _ := r.h.(WorkObjectHost)
		var share func()
		if sh, ok := r.h.(WorkObjectSharer); ok && wh != nil && sh.CanShareWorkObjects() {
			share = func() {
				wh.ShareWorkObject(WorkObjectOpen{Entity: e, AppUnfurlURL: wo.AppUnfurlURL, Channel: ch, TS: ts})
			}
		}
		anchor := ev.Anchor
		if ap, ok := r.h.(PointerHost); ok {
			anchor = ap.PointerPos()
		}
		ph.ShowOverlay(popups.NewWorkObjectMenu(ph, anchor, popups.WorkObjectMenuOpts{
			Entity: e, OpenURL: woOpenURL(wo),
			Preview: func() { open("details") },
			Share:   share,
			Open:    func(u string) { openURL(u) },
			Action:  func(b workobject.Button) { r.woAction(ev.Att, b, ch, ts) },
		}))
	}
}

// woButton is a footer button: its URL opens (aN: window.open) and,
// unless it only has a URL, the action goes to the app (aT).
func (r *richHandler) woButton(ev richview.WorkObjectEvent, ch, ts string) {
	b := ev.Button
	if b == nil {
		return
	}
	if b.URL == "" || b.AgentPrompt != "" {
		r.woAction(ev.Att, *b, ch, ts)
	}
	if b.URL != "" {
		openURL(richtext.SafeURL(b.URL))
	}
}

// woAction sends a layout action to the app: blocks.actions with the
// layout's block_payload (service_id, service_team_id, container type)
// in the attachment's container (aT).
func (r *richHandler) woAction(att *richtext.Attachment, b workobject.Button, ch, ts string) {
	app := r.h.Core()
	if app == nil || att == nil || att.WorkObject == nil {
		return
	}
	e := att.WorkObject.E
	lay := e.UnfurlLayout()
	if lay == nil || lay.Actions == nil {
		return
	}
	id := 0
	if att.ID != "" {
		id = atoiSafe(att.ID)
	}
	if id == 0 {
		return // "work object block action has no attachment id to dispatch with"
	}
	bp := lay.Actions.BlockPayload
	typ := bp.Container.Type
	if typ == "" {
		typ = "message_attachment"
	}
	action, _ := json.Marshal(b)
	threadTS := ""
	if r.m != nil && r.m.ThreadTS != r.m.TS {
		threadTS = r.m.ThreadTS
	}
	ix := app.Interactive()
	if ix == nil {
		return
	}
	req := core.BlockAction{ServiceID: bp.ServiceID, AppID: e.AppID, ServiceTeamID: bp.ServiceTeamID, Action: action,
		Container: core.ActionContainer{Type: typ, ChannelID: ch, MessageTS: ts, ThreadTS: threadTS,
			IsEphemeral: ix.Ephemeral(ch, ts), AttachmentID: id}}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ix.BlockActionSend(ctx, req)
	}()
}

// atoiSafe is a non-negative decimal (0 when it isn't one).
func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

var _ richview.WorkObjectHandler = (*richHandler)(nil)
