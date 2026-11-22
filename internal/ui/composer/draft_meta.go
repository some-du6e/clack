package composer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"log"
	"net"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A draft's file the composer knows by id alone. Karim (2026-11-17, #clack
// 1794887336.751199): a restored draft's attachment card read
// "F0CJB175J10" over "File". Slack's composer (build 031dbe6a7400) never
// shows an id:
//
//   - ComposerAttachments gives each draft file id to getFileById with
//     fetchIfMissing (client-boot 4ZwG): a file the store lacks is the
//     files UnknownFetchManager's unknown object {id, isUnknown} (CWC5,
//     0mwJ) while it fetches it (files.info, reason
//     "files-store-unknown-fetch"), in batches of up to 30, 200ms apart.
//   - PendingFile (cu6M) draws the file as MediumFileEntity (+SxN): an
//     unknown one is two MissingText bars (Qo+v: 8px pills 120 wide,
//     base-ter) in place of the name and the type line, over the empty
//     exception thumbnail (p-file_thumbnail__container--exception: the
//     sk_foreground_low_solid tile); once fetched, its name, its
//     getFileTypeDescription (the pretty_type) and its icon.
//   - fetchWithRetries tries each fetch 3 times when it times out (65s an
//     attempt); any other failure, a network one too, ends it, and the
//     files it didn't get become non-existent objects {id, isNonExistent}:
//     isTombstone, so the entity reads "This file was deleted." and the
//     thumbnail is the trash glyph.
//
// Each failed attempt logs the method, the error and the file's id hashed
// (a draft's files are the user's).

// attMetaAttempts is fetchWithRetries' MAX_FETCH_ATTEMPTS.
const attMetaAttempts = 3

var (
	attMetaTimeout = 65 * time.Second // FETCH_TIMEOUT_MS (shorter in tests)
	attMetaBackoff = time.Second      // before a transport error's next attempt
)

// attLogID is id hashed for the log: the first 12 hex digits of its
// SHA-256.
func attLogID(id string) string {
	h := sha256.Sum256([]byte(id))
	return "file#" + hex.EncodeToString(h[:6])
}

// attMetaRetry reports whether err is one fetchWithRetries tries again:
// a timeout (its FETCH_TIMEOUT_MS, or the network's). Anything else, an
// answer ({"ok":false}, an HTTP status) or a network failure, ends it.
func attMetaRetry(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// loadMeta is the unknown fetch for chip c: files.info, then the chip
// shows the file (its name, type and thumbnail), or after the attempts it
// is gone. Runs on its own goroutine.
func (a *attachments) loadMeta(c *attChip) {
	a.mu.Lock()
	id := ""
	if c.prepared != nil {
		id = c.prepared.ID
	}
	a.mu.Unlock()
	if id == "" || a.core == nil {
		a.metaDone(c, nil, false)
		return
	}
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), attMetaTimeout)
		f, err := a.core.FileInfo(ctx, id)
		cancel()
		if err == nil && (f.Name != "" || f.Title != "") {
			a.metaDone(c, &attMeta{name: f.Name, title: f.Title, pretty: f.PrettyType, thumb: f.Thumb, image: f.IsImage, size: f.Size}, true)
			return
		}
		if err == nil {
			err = errors.New("no file in the answer")
		}
		log.Printf("composer: files.info for a draft's file %s failed (attempt %d of %d): %v", attLogID(id), attempt, attMetaAttempts, err)
		if attempt >= attMetaAttempts || !attMetaRetry(err) {
			a.metaDone(c, nil, false)
			return
		}
		if !errors.Is(err, context.DeadlineExceeded) { // a network timeout: give it a moment
			time.Sleep(attMetaBackoff * time.Duration(attempt))
		}
	}
}

type attMeta struct {
	name, title, pretty, thumb string
	image                      bool
	size                       int64
}

// metaDone ends c's fetch: m is the file (ok), else c is gone.
func (a *attachments) metaDone(c *attChip, m *attMeta, ok bool) {
	a.mu.Lock()
	c.loading = false
	if !ok || m == nil {
		c.gone = true
	} else {
		name := attCleanName(firstNonEmpty(m.name, m.title))
		title := firstNonEmpty(m.title, name)
		c.name, c.title = name, title
		c.kind = attKindOf(name, nil)
		if m.pretty != "" {
			c.kind.Label = m.pretty
		}
		if m.image && m.thumb != "" {
			c.thumbURL, c.kind.Image = m.thumb, true
		}
		if c.prepared != nil {
			c.prepared.Name, c.prepared.Title = name, title
			if m.size > 0 {
				c.prepared.Size = m.size
			}
		}
	}
	a.mu.Unlock()
	a.h.Invalidate()
}

// drawMetaCard is the card of a file being fetched (two MissingText bars
// over the empty exception tile) or that never came (the trash glyph and
// "This file was deleted.").
func (a *attachments) drawMetaCard(gtx layout.Context, th *theme.Theme, v attView, pad, tile int, r image.Rectangle) {
	exc := rgb(0xdddddd) // sk_foreground_low_solid
	bar := rgb(0xf8f8f8) // dt_color-base-ter
	if th.Dark {
		exc, bar = rgb(0x35373b), rgb(0x2c2d30)
	}
	fillRRect(gtx, image.Rect(pad, pad, pad+tile, pad+tile), gtx.Dp(8), exc)
	tx := pad + tile + gtx.Dp(12)
	if v.loading {
		// MissingText (size medium: 120px, 8px tall, pill), one for the
		// name's line and one for the type's
		bw := min(gtx.Dp(120), max(r.Dx()-tx-gtx.Dp(12), 0))
		bh := gtx.Dp(8)
		for _, cy := range []int{pad + gtx.Dp(11), pad + gtx.Dp(22) + gtx.Dp(9)} {
			fillRRect(gtx, image.Rect(tx, cy-bh/2, tx+bw, cy-bh/2+bh), bh/2, bar)
		}
		return
	}
	ic := gtx.Dp(20)
	offset(gtx, pad+(tile-ic)/2, pad+(tile-ic)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("trash")(gtx, unit.Dp(20), th.C.FgMuted)
	})
	tw := max(r.Dx()-tx-gtx.Dp(12), 0)
	offset(gtx, tx, pad+gtx.Dp(9), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Dp(22))}
		return label(gtx, th.Shaper, textStyle{Size: 15, LineHeight: 22, Color: th.C.FgMuted, MaxLines: 1}, i18n.T("files", "This file was deleted."))
	})
}
