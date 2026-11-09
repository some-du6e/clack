package core

import (
	"context"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Background name resolution.
//
// Rendering records ids it could not name: channel mentions outside the
// sidebar, users not in the directory cache, bots without a profile. This
// loop resolves them in batches (edge channels/info for channels,
// conversations.info for DMs, edge users/info for users, bots.info for
// bots), caches the names as metadata and emits
// Event{Type:"names", Users: <resolved ids>} so views re-render (re-read
// History/Replies; Docs are rebuilt from the store).

func (a *App) resolveLoop(ctx context.Context) {
	notify := a.S.MissingNotify()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
		}
		time.Sleep(150 * time.Millisecond) // coalesce a burst (a page of messages)
		chans, users, bots := a.S.TakeMissing()
		if len(chans)+len(users)+len(bots) == 0 {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ids := a.resolve(rctx, chans, users, bots)
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
			out = append(out, c.ID)
		}
	}
	return out
}

// resolveBots caches bot names and icons (bots.info, comma-separated batch).
func (a *App) resolveBots(ctx context.Context, ids []string) []string {
	var out []string
	for i := 0; i < len(ids); i += 50 {
		j := min(i+50, len(ids))
		var r struct {
			Bots []slack.BotProfile `json:"bots"`
			Bot  *slack.BotProfile  `json:"bot"`
		}
		if err := a.C.Call(ctx, "bots.info", url.Values{"bots": {strings.Join(ids[i:j], ",")}}, &r); err != nil {
			log.Printf("bots.info: %v", err)
			continue
		}
		if r.Bot != nil {
			r.Bots = append(r.Bots, *r.Bot)
		}
		for _, b := range r.Bots {
			if b.ID == "" {
				continue
			}
			a.S.PutBot(b.ID, store.BotInfo{Name: b.Name, Icon: firstNonEmpty(b.Icons.Image72, b.Icons.Image48, b.Icons.Image36),
				AppID: b.AppID, Workflow: b.IsWorkflowBot})
			out = append(out, b.ID)
		}
	}
	return out
}

// ResolveConv returns any conversation by id: the sidebar's, or one looked
// up with conversations.info (closed DMs, channels opened from search or
// links), whose IM partner is then fetched. Results are cached in memory.
func (a *App) ResolveConv(ctx context.Context, id string) (Conv, bool) {
	if c, ok := a.Conv(id); ok {
		return c, true
	}
	var c *slack.Conversation
	err := a.once("ci:"+id, func() error {
		var err error
		c, err = a.C.ConversationInfo(ctx, id)
		return err
	})
	if err != nil || c == nil {
		if c2, ok := a.S.ExtConv(id); ok { // a concurrent caller stored it
			c = c2
		} else {
			return Conv{}, false
		}
	}
	if c.ID == "" {
		c.ID = id
	}
	if c.IsIM && c.User != "" {
		a.EnsureUsers(ctx, []string{c.User})
	}
	a.S.PutExtConv(*c)
	if id == a.selectedID() && !c.IsIM {
		a.emit(Event{Type: "sidebar_changed"}) // the open channel's row (preview.go)
	}
	return a.conv(c), true
}
