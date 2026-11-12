package core

// Work objects: the entity cards apps unfurl links as
// (internal/workobject), and what Slack's client keeps for them. From
// the CDN build 4d0c78085232 (2026-11-10):
//
//   - the experiments (client-boot hZia, boot-shared cAEk, client-boot
//     PEUt): work_objects_rearrangement, work_objects_refresh_timestamp,
//     wo_comment_sync_client_xp, work_objects_embedded_preview,
//     work_objects_message_search_xp_v2, third_party_integrations_video_previews
//   - details: ExternalWorkObjectEntity's flexpane asks
//     workObjects.getEntity (8Usa, ARiC fetchWorkObject: entity_id,
//     entity_url, channel_id, message_ts, reason, force_refetch); the
//     answer comes back as the entity_details_present RTM event (zuQH:
//     data_freshness "live" or ext_error), which upserts the entity into
//     the workObjects store (ARiC upsertWorkObjectsAndMarkAsFresh). Until
//     then the flexpane shows the unfurl's own data (addUnfurlCache,
//     transformUnfurlToWorkObject 77xh: fromUnfurlCache).
//   - comments (wo_comment_sync_client_xp): workObjects.getComments
//     (2QTP; entity_id, entity_url, cursor), answered by
//     entity_comments_present (the entity's comment list); posting is
//     workObjects.postComment (MkLN: entity_id, entity_url,
//     comment.blocks, thread_root_id), acknowledged by
//     entity_comment_action_acknowledged (zuQH
//     workObjectCommentActionAcknowledgedRtmHandler: post appends the
//     comment, edit merges it, delete drops it). The card's comment
//     button badges while comments.latest_timestamp is newer than the
//     latest the viewer saw (0Omc / eu u).
//   - conversations: entities.refs.list (D+Tv JJ: entity_id,
//     entity_url, reason "get-refs-for-entity") lists the messages that
//     shared the item (refs: channel_id, timestamp).
//   - refresh: the footer's Refresh is chat.refreshUnfurl (6Bms: channel,
//     ts, entity_id, reason "work-objects-manual-refresh"); the card says
//     "Refreshing…" until the unfurl comes back with a new updated_at,
//     and "Failed to refresh" for 2s after an error or 10s without one
//     (sSNM useRefresh).

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/workobject"
)

// The work object experiments (TestExperimentReadsListed).
const (
	ExpWORearrangement     = "work_objects_rearrangement"
	ExpWORefreshTimestamp  = "work_objects_refresh_timestamp"
	ExpWOCommentSync       = "wo_comment_sync_client_xp"
	ExpWOEmbeddedPreview   = "work_objects_embedded_preview"
	ExpWOSearch            = "work_objects_message_search_xp_v2"
	ExpWOVideoPreviews     = "third_party_integrations_video_previews"
	woRefreshTimeout       = 10 * time.Second
	woRefreshErrorShownFor = 2 * time.Second
)

func init() {
	for _, n := range []string{ExpWORearrangement, ExpWORefreshTimestamp, ExpWOCommentSync, ExpWOEmbeddedPreview, ExpWOSearch, ExpWOVideoPreviews} {
		experimentReads[n] = on
		// as experiments.getByUser had them on 2026-11-10 (docs/EXPERIMENTS.md)
		defaultExperimentGroups[n] = "on"
	}
}

// WorkObjectFlags are the work object experiments, as the cards read
// them ("on" === getGroupForUser).
type WorkObjectFlags struct {
	Rearrange, RefreshTimestamp, CommentSync, EmbeddedPreview, Search, VideoPreviews bool
}

// WorkObjectFlags reads the work object experiments.
func (a *App) WorkObjectFlags() WorkObjectFlags {
	return WorkObjectFlags{
		Rearrange:        a.Experiment(ExpWORearrangement) == "on",
		RefreshTimestamp: a.Experiment(ExpWORefreshTimestamp) == "on",
		CommentSync:      a.Experiment(ExpWOCommentSync) == "on",
		EmbeddedPreview:  a.Experiment(ExpWOEmbeddedPreview) == "on",
		Search:           a.Experiment(ExpWOSearch) == "on",
		VideoPreviews:    a.Experiment(ExpWOVideoPreviews) == "on",
	}
}

// WorkObjectDetails is the workObjects store's entry for an entity.
type WorkObjectDetails struct {
	EntityID  string
	EntityURL string
	// Entity is the live details (entity_details_present), or the unfurl's
	// own data until they arrive (FromUnfurl).
	Entity     *workobject.Entity
	FromUnfurl bool
	Loading    bool
	// Error is ext_error's message, or the failure ("Timeout while
	// fetching work object entity": get_entity_request_timed_out).
	Error, ErrorStatus string
	EventTS            string
}

// WorkObjectComment is one comment (entity_comments_present's comments).
type WorkObjectComment struct {
	ID        string          `json:"id"`
	Sender    workobject.User `json:"sender"`
	Timestamp json.RawMessage `json:"timestamp,omitempty"`
	Comment   struct {
		Blocks json.RawMessage `json:"blocks,omitempty"`
		Value  string          `json:"value,omitempty"`
	} `json:"comment"`
	CanEdit       bool              `json:"can_edit,omitempty"`
	CanDelete     bool              `json:"can_delete,omitempty"`
	CanReply      bool              `json:"can_reply,omitempty"`
	URL           string            `json:"url,omitempty"`
	ReplyCount    int               `json:"reply_count,omitempty"`
	ReplyUsers    []workobject.User `json:"reply_users,omitempty"`
	LatestReplyTS json.RawMessage   `json:"latest_reply_ts,omitempty"`
}

// WorkObjectComments is an entity's comments (the store's
// commentsByEntityId).
type WorkObjectComments struct {
	EntityID       string              `json:"entity_id"`
	Comments       []WorkObjectComment `json:"comments"`
	CanPostComment bool                `json:"can_post_comment"`
	NextCursor     string              `json:"next_cursor,omitempty"`
	// Loading: getComments asked, no answer yet; Failed: it failed or
	// timed out (WorkObjectCommentsErrorState).
	Loading, Failed bool `json:"-"`
	// Posting: a postComment is waiting for its acknowledgement;
	// PostFailed: the last one failed.
	Posting, PostFailed bool `json:"-"`
}

// WorkObjectRef is a message that shared an entity (entities.refs.list).
type WorkObjectRef struct {
	ChannelID string `json:"channel_id"`
	TS        string `json:"timestamp"`
}

type woRefresh struct {
	refreshing, failed bool
	updatedAt          float64
	until              time.Time // failed: shown until
	timer              *time.Timer
}

type woStore struct {
	mu       sync.Mutex
	gen      uint64
	details  map[string]*WorkObjectDetails
	comments map[string]*WorkObjectComments
	seen     map[string]float64 // entity → latest comment timestamp the viewer saw
	refs     map[string][]WorkObjectRef
	refresh  map[string]*woRefresh // channel/ts/entity
}

// woStates holds each App's work object store (kept off the App struct,
// as lists.go keeps its state, so parallel branches don't collide on it).
var woStates sync.Map

func (a *App) wo() *woStore {
	if v, ok := woStates.Load(a); ok {
		return v.(*woStore)
	}
	v, _ := woStates.LoadOrStore(a, &woStore{details: map[string]*WorkObjectDetails{}, comments: map[string]*WorkObjectComments{},
		seen: map[string]float64{}, refs: map[string][]WorkObjectRef{}, refresh: map[string]*woRefresh{}})
	return v.(*woStore)
}

// WorkObjectGen counts the store's changes (richview re-lays cards out
// when it moves).
func (a *App) WorkObjectGen() uint64 {
	s := a.wo()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// changed bumps the generation and tells the UI. s.mu is not held.
func (a *App) woChanged(entity string) {
	s := a.wo()
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
	a.emit(Event{Type: "work_objects", ID: entity})
}

// WorkObjectCardState is a card's live state: refreshing or failed (the
// footer), and whether newer comments wait (the comments badge).
func (a *App) WorkObjectCardState(ch, ts, entityID string, latestComment float64) (refreshing, failed, newer bool) {
	s := a.wo()
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.refresh[ch+"/"+ts+"/"+entityID]; r != nil {
		refreshing = r.refreshing
		failed = r.failed && time.Now().Before(r.until)
	}
	if latestComment > 0 {
		newer = latestComment > s.seen[entityID]
	}
	return
}

// RefreshWorkObject is the footer's Refresh (sSNM handleRefresh):
// chat.refreshUnfurl, "Refreshing…" until the unfurl's updated_at
// changes, "Failed to refresh" for 2s after an error or 10s.
func (a *App) RefreshWorkObject(ch, ts, entityID string, updatedAt float64) {
	if ch == "" || ts == "" || entityID == "" {
		return
	}
	s := a.wo()
	key := ch + "/" + ts + "/" + entityID
	s.mu.Lock()
	if r := s.refresh[key]; r != nil && r.refreshing {
		s.mu.Unlock()
		return
	}
	r := &woRefresh{refreshing: true, updatedAt: updatedAt}
	s.refresh[key] = r
	r.timer = time.AfterFunc(woRefreshTimeout, func() { a.woRefreshFailed(key, r) })
	s.mu.Unlock()
	a.woChanged(entityID)
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, woRefreshTimeout)
		defer cancel()
		p := url.Values{"channel": {ch}, "ts": {ts}, "entity_id": {entityID}, "reason": {"work-objects-manual-refresh"}}
		if err := a.C.Call(ctx, "chat.refreshUnfurl", p, nil); err != nil {
			a.woRefreshFailed(key, r)
		}
	}()
}

func (a *App) woRefreshFailed(key string, r *woRefresh) {
	s := a.wo()
	s.mu.Lock()
	if s.refresh[key] != r || !r.refreshing {
		s.mu.Unlock()
		return
	}
	r.refreshing, r.failed, r.until = false, true, time.Now().Add(woRefreshErrorShownFor)
	r.timer.Stop()
	s.mu.Unlock()
	entity := key[lastSlash(key)+1:]
	a.woChanged(entity)
	time.AfterFunc(woRefreshErrorShownFor, func() { a.woChanged(entity) })
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

// woMessageUpdated ends a refresh when the message's unfurl comes back
// with a new updated_at (useRefresh: updatedAt !== the one it started
// with). Called with each changed message.
func (a *App) woMessageUpdated(ch string, m *slack.Message) {
	s := a.wo()
	s.mu.Lock()
	if len(s.refresh) == 0 {
		s.mu.Unlock()
		return
	}
	var done []string
	for i := range m.Attachments {
		e := attachmentEntity(&m.Attachments[i])
		if e == nil {
			continue
		}
		key := ch + "/" + m.TS + "/" + e.EntityID
		r := s.refresh[key]
		if r == nil || !r.refreshing {
			continue
		}
		upd := 0.0
		if l := e.UnfurlLayout(); l != nil {
			upd = l.UpdatedAtUnix()
		}
		if upd != 0 && upd != r.updatedAt {
			r.refreshing = false
			r.timer.Stop()
			delete(s.refresh, key)
			done = append(done, e.EntityID)
		}
	}
	s.mu.Unlock()
	for _, id := range done {
		a.woChanged(id)
	}
}

// attachmentEntity is an attachment's work object (BOLE getWorkObject).
func attachmentEntity(at *slack.Attachment) *workobject.Entity {
	raw := at.WorkObjectEntity
	if len(raw) == 0 || string(raw) == "null" {
		raw = at.WorkObject
	}
	if len(raw) == 0 {
		return nil
	}
	return workobject.Parse(raw)
}

// ---- details ----

// WorkObjectRequest is what the flexpane fetches an entity with.
type WorkObjectRequest struct {
	EntityID, EntityURL string
	Channel, TS         string
	// Unfurl is the card's own data, shown until the live details come
	// (addUnfurlCache).
	Unfurl json.RawMessage
	Force  bool // force_refetch (the flexpane's Refresh)
	Reason string
}

// WorkObject returns the store's entry for an entity (nil if none).
func (a *App) WorkObject(entityID string) *WorkObjectDetails {
	s := a.wo()
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.details[entityID]; d != nil {
		c := *d
		return &c
	}
	return nil
}

// FetchWorkObject is fetchWorkObject: the entity's unfurl data goes in
// the store first, then workObjects.getEntity asks for the live details,
// which entity_details_present delivers; Slack gives the event 20s
// before it marks the entity get_entity_request_timed_out.
func (a *App) FetchWorkObject(r WorkObjectRequest) {
	if r.EntityID == "" {
		return
	}
	s := a.wo()
	s.mu.Lock()
	d := s.details[r.EntityID]
	if d == nil {
		d = &WorkObjectDetails{EntityID: r.EntityID}
		s.details[r.EntityID] = d
	}
	if d.Entity == nil && len(r.Unfurl) > 0 {
		if e := workobject.Parse(r.Unfurl); e != nil {
			d.Entity, d.FromUnfurl = e, true
		}
	}
	d.EntityURL = firstNonEmpty(r.EntityURL, d.EntityURL)
	if d.Loading && !r.Force {
		s.mu.Unlock()
		return
	}
	d.Loading, d.Error, d.ErrorStatus = true, "", ""
	s.mu.Unlock()
	a.woChanged(r.EntityID)
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 20*time.Second)
		defer cancel()
		p := url.Values{"entity_id": {r.EntityID}, "reason": {firstNonEmpty(r.Reason, "work-objects-conditional-fetch")}}
		if r.EntityURL != "" {
			p.Set("entity_url", r.EntityURL)
		}
		if r.Channel != "" {
			p.Set("channel_id", r.Channel)
		}
		if r.TS != "" {
			p.Set("message_ts", r.TS)
		}
		if r.Force {
			p.Set("force_refetch", "true")
		}
		if err := a.C.Call(ctx, "workObjects.getEntity", p, nil); err != nil {
			a.woDetailsDone(r.EntityID, "UnknownError")
			return
		}
		// the answer is the RTM event; without one in 20s Slack marks it
		// get_entity_request_timed_out
		time.AfterFunc(20*time.Second, func() {
			s.mu.Lock()
			late := s.details[r.EntityID] != nil && s.details[r.EntityID].Loading
			s.mu.Unlock()
			if late {
				a.woDetailsDone(r.EntityID, "get_entity_request_timed_out")
			}
		})
	}()
}

func (a *App) woDetailsDone(entityID, errCode string) {
	s := a.wo()
	s.mu.Lock()
	if d := s.details[entityID]; d != nil {
		d.Loading = false
		if errCode != "" {
			d.ErrorStatus = errCode
		}
	}
	s.mu.Unlock()
	a.woChanged(entityID)
}

// onWorkObjectEvent handles the work object RTM events.
func (a *App) onWorkObjectEvent(ev slack.Event) {
	switch ev.Type {
	case "entity_details_present":
		var e struct {
			EntityID      string          `json:"entity_id"`
			EntityURL     string          `json:"entity_url"`
			EntityDetails json.RawMessage `json:"entity_details"`
			DataFreshness string          `json:"data_freshness"`
			EventTS       string          `json:"event_ts"`
			ExtError      *struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"ext_error"`
		}
		if json.Unmarshal(ev.Raw, &e) != nil || e.EntityID == "" {
			return
		}
		if e.ExtError == nil && e.DataFreshness != "live" {
			return // only live details or errors upsert (zuQH)
		}
		s := a.wo()
		s.mu.Lock()
		d := s.details[e.EntityID]
		if d == nil {
			d = &WorkObjectDetails{EntityID: e.EntityID}
			s.details[e.EntityID] = d
		}
		d.Loading, d.EventTS = false, e.EventTS
		d.EntityURL = firstNonEmpty(e.EntityURL, d.EntityURL)
		if ent := workobject.Parse(e.EntityDetails); ent != nil {
			d.Entity, d.FromUnfurl = ent, false
		}
		if e.ExtError != nil {
			d.Error, d.ErrorStatus = e.ExtError.Message, e.ExtError.Status
		} else {
			d.Error, d.ErrorStatus = "", ""
		}
		s.mu.Unlock()
		a.woChanged(e.EntityID)
	case "entity_comments_present":
		var c WorkObjectComments
		if json.Unmarshal(ev.Raw, &c) != nil || c.EntityID == "" {
			return
		}
		s := a.wo()
		s.mu.Lock()
		if old := s.comments[c.EntityID]; old != nil {
			c.Posting, c.PostFailed = old.Posting, old.PostFailed
		}
		s.comments[c.EntityID] = &c
		s.mu.Unlock()
		a.woChanged(c.EntityID)
	case "entity_comment_action_acknowledged":
		var e struct {
			CommentAction string             `json:"comment_action"`
			EntityID      string             `json:"entity_id"`
			Comment       *WorkObjectComment `json:"comment"`
			CommentID     string             `json:"comment_id"`
			ThreadRootID  string             `json:"thread_root_id"`
			Error         json.RawMessage    `json:"error"`
		}
		if json.Unmarshal(ev.Raw, &e) != nil || e.EntityID == "" {
			return
		}
		failed := len(e.Error) > 0 && string(e.Error) != "null" && string(e.Error) != "false" && string(e.Error) != `""`
		s := a.wo()
		s.mu.Lock()
		c := s.comments[e.EntityID]
		if c != nil && e.ThreadRootID == "" { // thread replies live in their thread (not drawn by Clack yet)
			switch e.CommentAction {
			case "post":
				c.Posting = false
				if failed {
					c.PostFailed = true
				} else if e.Comment != nil {
					c.PostFailed = false
					c.Comments = append(c.Comments, *e.Comment)
				}
			case "edit":
				if !failed && e.Comment != nil {
					for i := range c.Comments {
						if c.Comments[i].ID == e.CommentID {
							merged := *e.Comment
							merged.ID = e.CommentID
							c.Comments[i] = merged
						}
					}
				}
			case "delete":
				if !failed {
					out := c.Comments[:0]
					for _, cm := range c.Comments {
						if cm.ID != e.CommentID {
							out = append(out, cm)
						}
					}
					c.Comments = out
				}
			}
		} else if c != nil && e.CommentAction == "post" {
			c.Posting = false
			if !failed && e.Comment != nil { // a reply: its root's count (addReplyUser)
				for i := range c.Comments {
					if c.Comments[i].ID == e.ThreadRootID {
						c.Comments[i].ReplyCount++
					}
				}
			}
		}
		s.mu.Unlock()
		a.woChanged(e.EntityID)
	}
}

// ---- comments ----

// WorkObjectCommentsOf returns an entity's comments (nil before
// getComments answers).
func (a *App) WorkObjectCommentsOf(entityID string) *WorkObjectComments {
	s := a.wo()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.comments[entityID]; c != nil {
		cp := *c
		cp.Comments = append([]WorkObjectComment(nil), c.Comments...)
		return &cp
	}
	return nil
}

// FetchWorkObjectComments is WorkObjectComments' fetch:
// workObjects.getComments (reason work-object-comments-fetch); the list
// arrives as entity_comments_present, or the tab says it couldn't load
// after 10s.
func (a *App) FetchWorkObjectComments(entityID, entityURL string) {
	if entityID == "" {
		return
	}
	s := a.wo()
	s.mu.Lock()
	c := s.comments[entityID]
	if c == nil {
		c = &WorkObjectComments{EntityID: entityID}
		s.comments[entityID] = c
	}
	c.Loading, c.Failed = true, false
	s.mu.Unlock()
	a.woChanged(entityID)
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 10*time.Second)
		defer cancel()
		p := url.Values{"entity_id": {entityID}, "reason": {"work-object-comments-fetch"}}
		if entityURL != "" {
			p.Set("entity_url", entityURL)
		}
		err := a.C.Call(ctx, "workObjects.getComments", p, nil)
		time.AfterFunc(func() time.Duration {
			if err != nil {
				return 0
			}
			return 10 * time.Second
		}(), func() {
			s.mu.Lock()
			cur := s.comments[entityID]
			stuck := cur != nil && cur.Loading && cur.Comments == nil
			if cur != nil {
				cur.Loading = false
				if stuck {
					cur.Failed = true
				}
			}
			s.mu.Unlock()
			if stuck {
				a.woChanged(entityID)
			}
		})
	}()
}

// PostWorkObjectComment is WorkObjectCommentComposer's send:
// workObjects.postComment with the composer's rich_text block; the
// comment shows once entity_comment_action_acknowledged says so.
func (a *App) PostWorkObjectComment(entityID, entityURL string, block json.RawMessage) {
	if entityID == "" || len(block) == 0 {
		return
	}
	s := a.wo()
	s.mu.Lock()
	if c := s.comments[entityID]; c != nil {
		c.Posting, c.PostFailed = true, false
	}
	s.mu.Unlock()
	a.woChanged(entityID)
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		defer cancel()
		cm, _ := json.Marshal(map[string]json.RawMessage{"blocks": block})
		p := url.Values{"entity_id": {entityID}, "comment": {string(cm)}, "reason": {"work-object-comment-post"}}
		if entityURL != "" {
			p.Set("entity_url", entityURL)
		}
		if err := a.C.Call(ctx, "workObjects.postComment", p, nil); err != nil {
			s.mu.Lock()
			if c := s.comments[entityID]; c != nil {
				c.Posting, c.PostFailed = false, true
			}
			s.mu.Unlock()
			a.woChanged(entityID)
		}
	}()
}

// SeeWorkObjectComments records the comments as seen (the Comments tab
// left, or the card mounted with them: 0Omc C / X): the badge goes.
func (a *App) SeeWorkObjectComments(entityID string, latest float64) {
	if entityID == "" || latest <= 0 {
		return
	}
	s := a.wo()
	s.mu.Lock()
	if s.seen[entityID] >= latest {
		s.mu.Unlock()
		return
	}
	s.seen[entityID] = latest
	s.mu.Unlock()
	a.woChanged(entityID)
}

// ---- conversations ----

// WorkObjectRefs returns the messages that shared an entity (nil before
// entities.refs.list answers).
func (a *App) WorkObjectRefs(entityID string) []WorkObjectRef {
	s := a.wo()
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]WorkObjectRef(nil), s.refs[entityID]...)
}

// FetchWorkObjectRefs is useGetRelatedThreads: entities.refs.list.
func (a *App) FetchWorkObjectRefs(entityID, entityURL string) {
	if entityID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		defer cancel()
		p := url.Values{"entity_id": {entityID}, "reason": {"get-refs-for-entity"}}
		if entityURL != "" {
			p.Set("entity_url", entityURL)
		}
		var out struct {
			Refs []struct {
				ChannelID string          `json:"channel_id"`
				Timestamp json.RawMessage `json:"timestamp"`
			} `json:"refs"`
		}
		if a.C.Call(ctx, "entities.refs.list", p, &out) != nil {
			return
		}
		var refs []WorkObjectRef
		for _, r := range out.Refs {
			ts := string(r.Timestamp)
			if s, err := strconv.Unquote(ts); err == nil {
				ts = s
			}
			refs = append(refs, WorkObjectRef{ChannelID: r.ChannelID, TS: ts})
		}
		s := a.wo()
		s.mu.Lock()
		s.refs[entityID] = refs
		s.mu.Unlock()
		a.woChanged(entityID)
	}()
}
