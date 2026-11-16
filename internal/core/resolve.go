package core

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Background name resolution.
//
// Rendering records ids it could not name: channel mentions outside the
// sidebar, users not in the members cache, bots without a profile. This
// loop resolves channels and bots in batches (edge channels/info for
// channels, conversations.info for DMs, bots.info for bots), caches the
// names as metadata and emits Event{Type:"names", Users: <resolved ids>}
// so views re-render (re-read History/Replies; Docs are rebuilt from the
// store). Users go to the member fetcher (members.go), which emits the
// same event.

func (a *App) resolveLoop(ctx context.Context) {
	notify := a.S.MissingNotify()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
		}
		time.Sleep(150 * time.Millisecond) // coalesce a burst (a page of messages)
		// users go to the member fetcher (members.go), on Slack's schedule
		chans, bots := a.S.TakeMissingNames()
		files, sections, previews := a.S.TakeMissingCanvases()
		recs := a.S.TakeMissingListRecords() // lists_records.go
		if len(chans)+len(bots)+len(files)+len(sections)+len(previews)+len(recs) == 0 {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var ids []string
		if len(chans)+len(bots) > 0 {
			ids = a.resolve(rctx, chans, nil, bots)
		}
		ids = append(ids, a.resolveCanvases(rctx, files, sections, previews)...) // canvas_refs.go
		ids = append(ids, a.resolveListRecords(rctx, recs)...)                   // lists_records.go
		cancel()
		if len(ids) > 0 {
			a.emit(Event{Type: "names", Users: ids})
		}
	}
}

// resolve looks up ids and returns those it could name.
func (a *App) resolve(ctx context.Context, chans, users, bots []string) []string {
	t0 := time.Now()
	var mu sync.Mutex
	var done []string
	add := func(ids ...string) {
		mu.Lock()
		done = append(done, ids...)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	var cs, ds []string
	for _, id := range chans {
		if strings.HasPrefix(id, "D") {
			ds = append(ds, id)
		} else {
			cs = append(cs, id)
		}
	}
	if len(cs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(a.resolveChannels(ctx, cs)...)
		}()
	}
	if len(ds) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem := make(chan struct{}, 4)
			var dwg sync.WaitGroup
			for _, id := range ds {
				dwg.Add(1)
				sem <- struct{}{}
				go func(id string) {
					defer dwg.Done()
					defer func() { <-sem }()
					if _, ok := a.ResolveConv(ctx, id); ok {
						add(id)
					}
				}(id)
			}
			dwg.Wait()
		}()
	}
	if len(users) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.EnsureUsers(ctx, users)
			for _, id := range users {
				if _, ok := a.S.User(id); ok {
					add(id)
				}
			}
		}()
	}
	if len(bots) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(a.resolveBots(ctx, bots)...)
		}()
	}
	wg.Wait()
	log.Printf("resolve: %d/%d names (%d channels, %d users, %d bots) in %s", len(done), len(chans)+len(users)+len(bots),
		len(chans), len(users), len(bots), time.Since(t0).Round(time.Millisecond))
	return done
}

// resolveChannels names channels via the edge cache (batches of 100).
func (a *App) resolveChannels(ctx context.Context, ids []string) []string {
	var out []string
	for i := 0; i < len(ids); i += 100 {
		j := min(i+100, len(ids))
		up := map[string]int{}
		for _, id := range ids[i:j] {
			up[id] = 0
		}
		var r struct {
			Results []slack.Conversation `json:"results"`
			Failed  []string             `json:"failed_ids"`
		}
		if err := a.C.Edge(ctx, "channels/info", map[string]any{"check_membership": true, "updated_ids": up}, &r); err != nil {
			log.Printf("edge channels/info: %v", err)
			a.S.ForgetMissing('c', ids[i:j]...)
			continue
		}
		for _, c := range r.Results {
			if c.ID == "" {
				continue
			}
			a.S.PutName(c.ID, c.Name)
			acc := richtext.ChannelVisible
			if c.IsPrivate && !c.IsMPIM {
				acc = richtext.ChannelPrivate
			}
			a.S.SetChannelAccess(c.ID, acc)
			out = append(out, c.ID)
		}
		// failed_ids: channels the viewer can't see (private ones they're
		// not in, other workspaces'): Slack's "Private channel" chip
		for _, id := range r.Failed {
			a.S.SetChannelAccess(id, richtext.ChannelHidden)
			out = append(out, id)
		}
	}
	return out
}

// resolveBots is Slack's fetchBotsById (client-boot 0H7E y, build
// 2c0d02ca130f), the bots UnknownFetchManager's fetch for a client with a
// socket: the edge cache's users/info (flannel MEMBER_INFO, iFK2
// fetchBotsById: chunks of 100, every id at 0 in updated_ids) first, then
// bots.info for the ids it didn't return, with app_ids lined up with them
// when a message that drew one carried its app (getBotByMsg's app_id).
// Slack's fallback covers the edge's failed_ids and failed chunks; Clack
// sends bots.info for any id the edge didn't answer with a bot.
//
// bots.info goes to the enterprise host on Grid (the team host answers
// bots_not_found for org apps); ids it doesn't know (bots_not_found, or
// left out of a partial answer) are asked once more on the team host,
// which knows the workspace's own bots. An id neither answers is left
// unresolved for the session, as Slack's upsertNonExistent leaves it; a
// request that failed outright (the network, a 5xx, a timeout) forgets
// its ids so the next draw asks again.
func (a *App) resolveBots(ctx context.Context, ids []string) []string {
	got := map[string]bool{}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	put := func(bs []slack.BotProfile) {
		for _, b := range bs {
			if b.ID == "" || !want[b.ID] || got[b.ID] {
				continue
			}
			got[b.ID] = true
			a.S.PutBot(b.ID, store.BotInfo{Name: b.Name, Icon: firstNonEmpty(b.Icons.Image72, b.Icons.Image48, b.Icons.Image36),
				AppID: b.AppID, Workflow: b.IsWorkflowBot})
			out = append(out, b.ID)
		}
	}
	left := func(part []string) []string {
		var rest []string
		for _, id := range part {
			if !got[id] {
				rest = append(rest, id)
			}
		}
		return rest
	}
	for i := 0; i < len(ids); i += 100 {
		part := ids[i:min(i+100, len(ids))]
		bs, err := a.edgeBots(ctx, part)
		if err != nil {
			log.Printf("edge users/info %v: %v", part, err)
		}
		put(bs)
	}
	rest := left(ids)
	var transient []string
	for i := 0; i < len(rest); i += 50 {
		part := rest[i:min(i+50, len(rest))]
		bs, err := a.botsInfo(ctx, part, a.C.Call)
		put(bs)
		if err != nil {
			log.Printf("bots.info %v: %v", part, err)
		}
		missing := left(part)
		if len(missing) > 0 && a.C.OnEnterprise("bots.info") && (err == nil || isCode(err, "bots_not_found")) {
			bs, err2 := a.botsInfo(ctx, missing, a.C.CallTeam)
			put(bs)
			if err2 != nil {
				log.Printf("bots.info %v (team host): %v", missing, err2)
			}
			err = err2
			missing = left(missing)
		}
		if err != nil && !isAPIError(err) {
			transient = append(transient, missing...)
		}
	}
	if len(transient) > 0 {
		a.S.ForgetMissing('b', transient...) // asked again at the next draw
	}
	return out
}

// edgeBots is fetchBotsById's flannel step: the bots users/info returns
// among ids.
func (a *App) edgeBots(ctx context.Context, ids []string) ([]slack.BotProfile, error) {
	up := make(map[string]int, len(ids))
	for _, id := range ids {
		up[id] = 0
	}
	var r struct {
		Results []slack.BotProfile `json:"results"`
	}
	err := a.C.Edge(ctx, "users/info", map[string]any{"updated_ids": up}, &r)
	return r.Results, err
}

// botsInfo is fetchBotsInfo: bots.info {bots, app_ids} through call.
func (a *App) botsInfo(ctx context.Context, ids []string, call func(context.Context, string, url.Values, any) error) ([]slack.BotProfile, error) {
	p := url.Values{"bots": {strings.Join(ids, ",")}}
	apps := make([]string, len(ids))
	known := false
	for i, id := range ids {
		apps[i] = a.S.MissingBotApp(id)
		known = known || apps[i] != ""
	}
	if known {
		p.Set("app_ids", strings.Join(apps, ","))
	}
	var r struct {
		Bots []slack.BotProfile `json:"bots"`
		Bot  *slack.BotProfile  `json:"bot"`
	}
	err := call(ctx, "bots.info", p, &r)
	if r.Bot != nil {
		r.Bots = append(r.Bots, *r.Bot)
	}
	return r.Bots, err
}

// isAPIError reports whether err is an answer from Slack ({"ok":false}),
// not a request that failed on the way.
func isAPIError(err error) bool {
	var se *slack.Error
	return errors.As(err, &se)
}

// ResolveConv returns any conversation by id: the sidebar's, or one looked
// up with conversations.info (closed DMs, channels opened from search or
// links), whose IM partner is then fetched. Results are cached in memory.
// A known channel without a name (a partial object some reply carried) is
// looked up again, like Slack's getChannelById fetching an unknown stub;
// a member conversation keeps its place in the sidebar's set. One
// conversations.info doesn't know (channel_not_found: a private channel
// the viewer isn't in, another workspace's) is recorded as hidden, the
// non-existent object Slack's fetch leaves (channel_lookup.go).
func (a *App) ResolveConv(ctx context.Context, id string) (Conv, bool) {
	if c, ok := a.Conv(id); ok && named(c) {
		return c, true
	}
	var c *slack.Conversation
	err := a.once("ci:"+id, func() error {
		var err error
		c, err = a.C.ConversationInfo(ctx, id)
		return err
	})
	if isCode(err, "channel_not_found") {
		a.S.SetChannelAccess(id, richtext.ChannelHidden)
	}
	if err != nil || c == nil {
		if v, ok := a.Conv(id); ok && (named(v) || err != nil) { // a concurrent caller stored it, or it stays partial
			return v, true
		}
		return Conv{}, false
	}
	if c.ID == "" {
		c.ID = id
	}
	if c.IsIM && c.User != "" {
		a.EnsureUsers(ctx, []string{c.User})
	}
	if _, member := a.S.Conv(id); member {
		a.S.PutConv(*c)
	} else {
		a.S.PutExtConv(*c)
	}
	if id == a.selectedID() && !c.IsIM {
		a.emit(Event{Type: "sidebar_changed"}) // the open channel's row (preview.go)
	}
	return a.conv(c), true
}

// named reports whether a conversation has what its name is drawn from:
// an IM its partner, anything else a name that isn't its own id (what a
// stub carries when the payload had no name, and what the store's
// ChannelName falls back to).
func named(c Conv) bool {
	if c.Kind == "im" {
		return c.User != ""
	}
	return c.Name != "" && c.Name != c.ID
}
