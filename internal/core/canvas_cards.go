package core

// A canvas's link cards (docs/CANVASES.md "Link cards"): Quip's export
// holds only a card's placeholder ("Link URL: <url>"), and Slack's client
// draws the card from Quip's model instead: a TypeLinkUnfurlCard section
// whose ContentLinkUnfurlCard names an unfurl container message
// ("suo:<team>/<channel>/<ts>"), fetched as ensureMessages fetches what
// its store lacks (messages.list by id) and drawn by Slack's Card
// (canvasview linkcard.go).

import (
	"context"
	"strings"

	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/slack"
)

// CanvasCard is one of the export's link cards as Quip's model has it.
type CanvasCard struct {
	URL string
	// Section is the card's section id ("" when the model has none for
	// the export's card); Type its ContentLinkUnfurlCard type.
	Section string
	Type    int32
	// Incomplete is the card's incomplete (a pending URL that won't
	// unfurl: Slack's "empty").
	Incomplete bool
	// ContentID is the card content's own id: a SLACK_FILE card's file,
	// a SLACK_DOCUMENT card's canvas, a SLACK_USER card's member (Quip's
	// "sf:", "sd:" or "su:" prefix dropped).
	ContentID string
	// Channel and TS are a SLACK_UNFURL card's unfurl container ("" when
	// its slack_unfurl_id isn't one).
	Channel, TS string
	// Sig is what the section holds: a change (a document_update) asks
	// for the container again.
	Sig string
}

// HasContainer: the card names an unfurl container message.
func (c CanvasCard) HasContainer() bool { return c.Channel != "" && c.TS != "" }

// CanvasCards pairs the export's link cards (their urls, in document
// order) with the model's card sections by url, the next unused one each
// (quip.PairLinkCards): ok is false while core holds no model of canvas
// id (loading, or failed: CanvasModelError).
func (a *App) CanvasCards(id string, urls []string) (cards []CanvasCard, ok bool) {
	a.CanvasModel(id, func(d *quip.Doc) {
		if d == nil {
			return
		}
		ok = true
		cards = make([]CanvasCard, len(urls))
		for i, s := range quip.PairLinkCards(d.LinkCards(), urls) {
			cards[i].URL = urls[i]
			if s == nil {
				continue
			}
			lc := s.Content.LinkUnfurl
			c := &cards[i]
			c.Section, c.Type = *s.ID, lc.CardType()
			c.Incomplete = lc.Incomplete != nil && *lc.Incomplete
			if cc := lc.CardContent; cc != nil {
				switch {
				case cc.SlackFile != nil && cc.SlackFile.SlackFileID != nil:
					c.ContentID = *cc.SlackFile.SlackFileID
				case cc.SlackDocument != nil && cc.SlackDocument.ID != nil:
					c.ContentID = *cc.SlackDocument.ID
				case cc.SlackUser != nil && cc.SlackUser.ID != nil:
					c.ContentID = *cc.SlackUser.ID
				}
				if i := strings.IndexByte(c.ContentID, ':'); i >= 0 && i <= 3 {
					c.ContentID = c.ContentID[i+1:] // syncer ids: "sf:F…", "sd:F…", "su:U…"
				}
			}
			if c.Type == quip.CardSlackUnfurl {
				if _, ch, ts, ok := quip.ParseUnfurlID(lc.UnfurlID()); ok {
					c.Channel, c.TS = ch, ts
				}
			}
			c.Sig = string(quip.Marshal(lc))
		}
	})
	return cards, ok
}

// FetchCanvasCardContainers fetches the cards' unfurl containers the
// store doesn't hold, as Slack's "Fetch and upsert messages" thunk does
// for the Quip delegate (ensureMessages: messages.list by id, grouped by
// channel), and waits for the answers or ctx. With again it asks for every
// container anew (a document_update changed their sections).
func (a *App) FetchCanvasCardContainers(ctx context.Context, cards []CanvasCard, again bool) {
	var keys []MsgKey
	for _, c := range cards {
		if c.HasContainer() {
			keys = append(keys, MsgKey{c.Channel, c.TS})
		}
	}
	if !again {
		a.EnsureMessages(ctx, keys)
		return
	}
	for _, k := range keys {
		a.RefetchMessages(k.Channel, []string{k.TS})
	}
	for _, ch := range a.refetchWaiters(keys) {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}

// CanvasCardContainer is the store's copy of the card's unfurl container
// (false: the store doesn't hold it, yet or any more).
func (a *App) CanvasCardContainer(c CanvasCard) (slack.Message, bool) {
	if !c.HasContainer() {
		return slack.Message{}, false
	}
	return a.S.FindHeld(c.Channel, c.TS)
}

// CardFileInfo is files.info's file for a canvas file card (Slack's file
// object fetcher, by id): never files.open.
func (a *App) CardFileInfo(ctx context.Context, id string) (slack.File, error) {
	return a.fileInfo(ctx, id)
}

// IsAPIError reports a Slack API refusal with one of codes.
func IsAPIError(err error, codes ...string) bool { return isCode(err, codes...) }
