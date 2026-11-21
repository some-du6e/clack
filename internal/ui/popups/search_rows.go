package popups

// The results page's Files, Channels and People results, and the search
// modal's "Results feel off? Learn more", as Slack's web client draws
// them: gantry-v2 `search-view_tsx` (00e7299087c81505) and boot-shared
// (4cdc1abea1047da8) of build c07ab895506f, read from the public CDN
// without a cookie; styles from client-boot-styles cf726c4f2eaecc74,
// whose class hashes are the ones build 2c0d02ca130f's JS names.
//
// Each tab hands its page to UnifiedSearchInteractiveList (an
// InteractiveList of ListItems, no gaps of their own; the megaphone
// banner it can slot after the second row is a promotion Clack doesn't
// show) inside a `margin_top_50` box:
//
//   - Files (FilesListItem → SearchFile cDKA, isFileResult and fullWidth,
//     → PillowFile 1JzM → MediumFileEntity UZv6): every file is its own
//     card. `.p-search_file--result` and `--last`/`--single` make it a
//     1px rgba(sk_primary_foreground, .1) box with radius 8
//     (`.p-search_file--ia4 .c-pillow_file`), 8px under it
//     (`.p-search_file.c-search__result`); `.c-pillow_file--full_width`
//     pads it 16 16 16 12. Inside, BaseEntity medium with the 36px
//     FileEntityThumbnailIcon (the image itself for an image, ringed
//     1px fg_low .13 at radius 8) 8px from the text
//     (`.p-search_file--result … .c-base_entity__avatar`), the column
//     pulled up and down 4 and centred on the icon: the title
//     (`c-file_entity__text--first-line`, 15px bold, line-height
//     1.46668, margin-top -4) and FileEntityUserTimestamp
//     (`c-file_entity-user-timestamp`: 13px, line-height 1.2, 2px down,
//     sk_foreground_max_solid): "Shared by {name}" ("Created by" with a
//     creator: filter) and "on {date}" / "today" (displaySharedBy,
//     excludeTime). Hover (`.fileListItem__UhvDr:hover`, `.c-pillow_file:
//     hover`): theme-surf-pry-hover over the card, the border otl-sec, a
//     0 1px 2px #0000000c shadow, and the detail line becomes the file's
//     action text (FileEntityMetadata's isHovering).
//   - Channels (ChannelListItem → ChannelSearchResult SgDH →
//     MediumChannelEntity macg + BrowsePageChannelSubtext POtT): the rows
//     touch: `.c-channel_search_result__channel` is padding 16 with a 1px
//     rgba(sk_foreground_low, .13) border but no bottom one, radius 8 8 0
//     0 on the first row, and the last row's 1px rgba(sk_primary_fore-
//     ground, .1) bottom and 0 0 8 8. The name row (15px, line-height
//     1.46668): InlineChannelIcon, the name in bold (margin-right 8,
//     underlined under the pointer), "View channel" (13px, sk_foreground_
//     max .7, margin 0 8; hidden but laid out until the row is hovered and
//     its buttons aren't), the inaction label (Archived / Required, 13px,
//     .7, an inline info icon and its tooltip; shown on hover) and the
//     mention badge (margin-left 8, 3px down). Its subtext is hidden
//     (`.c-channel_search_result__channel .c-base_entity__subtext`); the
//     metadata 4px under it (13px, line-height 1.25, content-sec, one line
//     with an ellipsis): "✓ Joined" (lilypad green, check-small, Joined
//     bold), "{n} members" and the purpose, "  ·  " between them. Hover
//     (`.channelListItem__gkOey:hover`): theme-surf-pry-hover and a 0 1px
//     1px #0000000f shadow; then the actions show (`.c-channel_search_
//     result__actions`: 12px in from the top right, padding 4): the
//     outline "Open in Home" (margin-right 8) and the outline Join, or
//     Joined that reads Leave under the pointer, or for a channel
//     isChannelEligibleForTemporaryJoin (OtvL) the split Join with its
//     "Join…" caret (Just for today, For 48 hours, For 1 week). Medium
//     buttons: 36 tall, min-width 80, padding 0 12, radius 8; the caret
//     min-width 32 without its left border.
//   - People (PeopleGrid → UserProfileUnfurl uS1K with isUnifiedPeople-
//     Result): a Grid (TRg4) of `repeat(auto-fill, minmax(250px, 1fr))`
//     columns 20 apart, rows as tall as their tallest card (cellHeight
//     only feeds the stretch math). The card (`.profileCard__KRk6t`):
//     base-pry, a 1px rgba(sk_foreground_low, .13) ring, radius 8; under
//     the pointer `.p-embedded_user_profile_unfurl_unified_hover` paints
//     surf-pry. MemberProfileBaseEntity (Se3V) padded 12
//     (`.p-embedded_user_profile_unfurl .c-base_entity`): the 72px avatar,
//     12px, then the column (margin -4, centred on the avatar): the name
//     (15/900) and presence, the title (15px, sk_primary_foreground, one
//     line) and the pronouns 4px under it (13px, sk_foreground_max .7).
//     With a local time the secondary block (`__secondary`: 1px fg_low .13
//     top border, padding 12): LocalTime's clock (20, margin-right 8) and
//     "{time} local time". Last MemberProfileHoverBanner: Workspace Owner,
//     Workspace Admin or Deactivated account (padding 8 16, 13px bold).
//     A click opens the member's profile.
//
// Not drawn: the loading skeletons Slack shows while a tab loads, the
// title's search-term highlights, files' hover action buttons (FileActions:
// Download, More actions), the shared-channel line under a Slack Connect
// channel, the people cards' location line (it needs the profile's
// sections) and keyboard focus within the lists.

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The tokens the rows paint with (client-boot-styles; light and dark
// values from the default themes).
var (
	srTokRowHover   = chipTok{"var(--dt_color-theme-surf-pry-hover)", rgba8(0x83388a, 0x0f), rgba8(0xefe1f5, 0x14)}
	srTokOtlSec     = chipTok{"var(--dt_color-otl-sec)", rgba8(0x5e5d60, 0x73), rgba8(0x797c81, 0x80)}
	srTokFgLow13    = chipTok{"rgba(var(--sk_foreground_low),0.13)", rgba8(0x1d1c1d, 0x21), rgba8(0xe8e8e8, 0x21)}
	srTokFgPry10    = chipTok{"rgba(var(--sk_primary_foreground),0.1)", rgba8(0x1d1c1d, 0x1a), rgba8(0xd1d2d3, 0x1a)}
	srTokFgMaxSolid = chipTok{"rgb(var(--sk_foreground_max_solid))", rgb(0x616061), rgb(0xababad)}
	srTokFgMax70    = chipTok{"rgba(var(--sk_foreground_max),0.7)", rgba8(0x1d1c1d, 0xb3), rgba8(0xe8e8e8, 0xb3)}
	srTokFgPry      = chipTok{"rgb(var(--sk_primary_foreground))", rgb(0x1d1c1d), rgb(0xd1d2d3)}
	srTokSurfPry    = chipTok{"var(--dt_color-surf-pry)", rgba8(0x1d1c1d, 0x0f), rgba8(0xf8f8f8, 0x0f)}
	srTokPryBg      = chipTok{"rgb(var(--sk_primary_background))", rgb(0xffffff), rgb(0x1a1d21)}
)

// ---- the search modal's footer links ----

// srSearchHelpArticle is BaseSearchHelpLink's articleId 0xc125828.
const srSearchHelpArticle = "https://slack.com/help/articles/202528808"

// srOpenHelp is showHelpArticle: the article in the browser. Tests
// replace it.
var srOpenHelp = srOpenLink

// srShowFeedbackLink is RhHp's getShouldShowFeedbackLink: no
// has_hipaa_compliance, ml_opt_out or search_feedback_opt_out team pref
// (h/Bp xp, JS truthiness). Tests replace it.
var srShowFeedbackLink = func(h Host) bool {
	c := h.Core()
	if c == nil {
		return true
	}
	for _, k := range [...]string{"has_hipaa_compliance", "ml_opt_out", "search_feedback_opt_out"} {
		if raw, ok := c.TeamPref(k); ok && srTruthy(raw) {
			return false
		}
	}
	return true
}

// srTruthy is a JSON value's JavaScript truthiness.
func srTruthy(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// footerLinks is OmniswitcherContent's `c-search_modal__feedback_link_
// container` (absolute at the popover's bottom right, padding 14 16, 13px):
// with getShouldShowFeedbackLink the FeedbackLink ("Give feedback");
// without it "Results feel off? " and BaseSearchHelpLink's "Learn more",
// which closes the search and opens the help article, underlined under
// a11y_underline_button_link and the underline_links pref (CX15 bF).
func (p *SearchPopover) footerLinks(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) {
	if srShowFeedbackLink(p.h) {
		p.feedbackLink(gtx, th, cl, sz)
		return
	}
	if p.learnMore.Clicked(gtx) {
		p.Dismiss()
		srOpenHelp(p.h, srSearchHelpArticle)
	}
	desc, link := nsSearch.T("Results feel off?")+" ", nsSearch.T("Learn more")
	lw := textW(gtx, th, 13, theme.Regular, link)
	x := sz.X - dp(gtx, 16) - lw
	dw := textW(gtx, th, 13, theme.Regular, desc)
	at(gtx, image.Pt(x-dw, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Regular, cl.Fg, desc)
		})
	})
	ul := false
	if c := p.h.Core(); c != nil {
		ul = c.UnderlineButtonLinks()
	}
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &p.learnMore, nil, func(gtx layout.Context) layout.Dimensions {
			d := vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, link)
			})
			if ul || p.learnMore.Hovered() { // .c-link--underline, else :hover
				lh := dp(gtx, 16)
				y := (sz.Y-lh)/2 + lh - dp(gtx, 2)
				fill(gtx, image.Rect(0, y, d.Size.X, y+max(1, dp(gtx, 1))), cl.Link)
			}
			return d
		})
	})
}

// ---- Files ----

// srFileRowH is a file result: the card (16 + 36 + 16 inside two 1px
// borders) and the 8px under it.
func srFileRowH(gtx layout.Context) int {
	return 2*border1(gtx) + dp(gtx, 68) + dp(gtx, 8)
}

// fileRow draws Files / Canvases result i.
func (v *SearchView) fileRow(gtx layout.Context, th *theme.Theme, cl *Colors, i, px, cw int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	f := v.files[i]
	bw := border1(gtx)
	cardH := 2*bw + dp(gtx, 68)
	sz := image.Pt(cw, cardH)
	hover := v.rows[i].Hovered()
	at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
		r := image.Rectangle{Max: sz}
		rad := dp(gtx, 8)
		bg := srTokPryBg.of(th)
		line := theme.Over(srTokFgPry10.of(th), bg)
		inner := bg
		if hover {
			softShadow(gtx, r, rad, dp(gtx, 1), dp(gtx, 2), color.NRGBA{A: 0x0c})
			line = theme.Over(srTokOtlSec.of(th), bg)
			inner = theme.Over(srTokRowHover.of(th), bg)
		}
		rrect(gtx, r, rad, line)
		rrect(gtx, r.Inset(bw), max(0, rad-bw), inner)
		g := gtx
		g.Constraints = layout.Exact(sz)
		v.rows[i].Layout(g, func(gtx layout.Context) layout.Dimensions {
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
		ix, iy := bw+dp(gtx, 12), bw+dp(gtx, 16)
		at(gtx, image.Pt(ix, iy), func(gtx layout.Context) layout.Dimensions {
			return srFileTile(gtx, v.h, th, f, 36)
		})
		tx := ix + dp(gtx, 36) + dp(gtx, 8)
		tw := max(1, cw-bw-dp(gtx, 16)-tx)
		// the column: title box (22, margin-top -4) and the 2px + 15.6px
		// detail line, its -4/-4 margins centred on the 36px icon
		top := iy + dp(gtx, (36-(22-4+2+15.6-8))/2-4)
		at(gtx, image.Pt(tx, top-dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 22))}
			if sp := srCanvasTitle(v.h, f); sp != nil {
				return srTitleSpans(gtx, v.h, th, sp, 15, 22, theme.Bold, cl.Fg, tw)
			}
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: cl.Fg, LineHeight: 22, MaxLines: 1}, srFileTitle(f))
		})
		at(gtx, image.Pt(tx, top+dp(gtx, 18+2)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 15.6))}
			col := srTokFgMaxSolid.of(th)
			if hover {
				return srRichLine(gtx, th, 13, 15.6, col, srFileActionParts(f), tw)
			}
			return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: col, LineHeight: 15.6, MaxLines: 1},
				srFileDetail(v.h, f, v.q.Get("creator") != "", time.Now()))
		})
		return layout.Dimensions{Size: sz}
	})
	return layout.Dimensions{Size: image.Pt(px+cw, srFileRowH(gtx))}
}

// srCanvasTitle is a canvas result's title as Slack's canvas titles draw
// it (richtext.CanvasTitle: title_blocks, else the title's :emoji:), when
// it holds an emoji; nil for any other result, whose title is text.
func srCanvasTitle(h Host, f core.File) []richtext.Span {
	if !f.IsCanvas || h.Core() == nil {
		return nil
	}
	sp := richtext.CanvasTitle(h.Core().S, strings.TrimSpace(f.Title), f.TitleBlocks)
	for _, s := range sp {
		if s.Kind == richtext.Emoji {
			return sp
		}
	}
	return nil
}

// srTitleSpans draws a title's spans on one line, w wide at most: text in
// col (a mention plain, as .p-quip_title__blocks_simple draws it) and
// emoji as images at the line's height (.p-quip_title__blocks__base
// .c-emoji--inline: 22px), one the store couldn't name when converted
// resolved now, else its ":name:".
func srTitleSpans(gtx layout.Context, h Host, th *theme.Theme, spans []richtext.Span, size, lh float32, wt font.Weight, col color.NRGBA, w int) layout.Dimensions {
	x := 0
	lineH := dp(gtx, lh)
	es := dp(gtx, 22)
	for _, sp := range spans {
		if sp.Kind == richtext.Break || x >= w {
			break
		}
		if sp.Kind == richtext.Emoji {
			url := sp.URL
			if sp.KeptEmoji() {
				url = emojiURL(h, sp.ID)
			}
			if url != "" {
				if x+es > w {
					break
				}
				u := url
				at(gtx, image.Pt(x, (lineH-es)/2), func(gtx layout.Context) layout.Dimensions {
					if img, ok := h.Images().Get(u, es); ok {
						return containImage(gtx, img, image.Pt(es, es))
					}
					return layout.Dimensions{Size: image.Pt(es, es)}
				})
				x += es
				continue
			}
		}
		s := sp.Text
		if sp.Kind == richtext.Emoji && s == "" {
			s = ":" + sp.ID + ":"
		}
		s = strings.ReplaceAll(s, "\n", " ")
		if s == "" {
			continue
		}
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-x), lineH)}
			return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: wt, Color: col, LineHeight: unit.Sp(lh), MaxLines: 1}, s)
		})
		x += d.Size.X
	}
	return layout.Dimensions{Size: image.Pt(min(x, w), lineH)}
}

// srFileTitle is the file's title, else its name.
func srFileTitle(f core.File) string {
	if t := strings.TrimSpace(f.Title); t != "" {
		return t
	}
	return f.Name
}

// srFileTile is FileEntityThumbnailIcon at s dp: an image's thumb (with
// the img-border ring), else the type tile (views' fileTileStyle).
func srFileTile(gtx layout.Context, h Host, th *theme.Theme, f core.File, s float32) layout.Dimensions {
	px := dp(gtx, s)
	sz := image.Pt(px, px)
	rad := dp(gtx, 8)
	if f.IsImage && f.Thumb != "" && h.Images() != nil {
		if img, ok := h.Images().Get(f.Thumb, 2*px); ok {
			theme.Image(gtx, img, sz, rad)
		} else {
			th.Placeholder(gtx, sz, rad)
		}
		stroke(gtx, image.Rectangle{Max: sz}, rad, border1(gtx), srTokFgLow13.of(th))
		return layout.Dimensions{Size: sz}
	}
	bg, fg, name := srFileTileStyle(f, th != nil && th.Dark)
	rrect(gtx, image.Rectangle{Max: sz}, rad, bg)
	g := dp(gtx, 20)
	at(gtx, image.Pt((px-g)/2, (px-g)/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, name, 20, fg)
	})
	return layout.Dimensions{Size: sz}
}

// srFileTileStyle is the per-type tile (views/files.go fileTileStyle).
func srFileTileStyle(f core.File, dark bool) (bg, fg color.NRGBA, name string) {
	white := rgb(0xffffff)
	ft := f.Filetype
	in := func(set ...string) bool {
		for _, s := range set {
			if ft == s {
				return true
			}
		}
		return false
	}
	switch {
	case f.IsList:
		if dark {
			return rgb(0xaa6c00), rgb(0x361e00), "list-view"
		}
		return rgb(0xe19b06), rgb(0xfcf2d9), "list-view"
	case f.IsCanvas:
		return rgb(0x1d9bd1), white, "canvas"
	case ft == "pdf":
		return rgb(0xe01e5a), white, "files"
	case in("xls", "xlsx", "csv", "gsheet", "ods", "numbers") || strings.Contains(f.Mime, "spreadsheet"):
		return rgb(0x2eb67d), white, "list-view"
	case in("ppt", "pptx", "gpres", "key", "odp") || strings.Contains(f.Mime, "presentation"):
		return rgb(0xe8912d), white, "image"
	case strings.HasPrefix(f.Mime, "image/"):
		return rgb(0x1264a3), white, "image"
	case strings.HasPrefix(f.Mime, "video/"):
		return rgb(0x1264a3), white, "video"
	case strings.HasPrefix(f.Mime, "audio/"):
		return rgb(0x7c3085), white, "volume"
	case f.Mode == "snippet" || ft == "text" || in("go", "javascript", "python", "c", "cpp", "java", "rust", "shell", "json", "yaml", "html", "css", "markdown", "typescript", "ruby", "php", "sql", "swift", "kotlin"):
		if dark {
			return rgb(0x3b3d42), white, "code"
		}
		return rgb(0x1d1c1d), white, "code"
	case in("doc", "docx", "gdoc", "odt", "rtf", "pages", "md") || strings.Contains(f.Mime, "wordprocessing") || strings.Contains(f.Mime, "msword"):
		return rgb(0x1264a3), white, "files"
	}
	return rgb(0x616061), white, "files"
}

// srFileDetail is FileEntityUserTimestamp with displaySharedBy and
// excludeTime: MemberName's "Shared by {name}" ("Created by {name}" with
// isCreator: a creator: filter), a space, then "on {date}" or "today" /
// "yesterday" for getFileTimestamp (updated, else created).
func srFileDetail(h Host, f core.File, creator bool, now time.Time) string {
	by := ""
	if f.User != "" {
		name := f.User
		if u, ok := lookupUser(h, f.User); ok {
			name = userName(u)
		}
		src := "Shared by {name}"
		if creator {
			src = "Created by {name}"
		}
		by = i18n.T("member_profile", src, "name", name)
	}
	ts := f.Modified
	if ts <= 0 {
		ts = f.Created
	}
	if ts <= 0 {
		return by
	}
	t := time.Unix(ts, 0)
	day := srCalendarDay(t, now)
	when := i18n.T("files", "on {date}", "date", day)
	if srSameDay(t, now) || srSameDay(t, now.AddDate(0, 0, -1)) {
		when = i18n.T("files", "{newDate}", "newDate", strings.ToLower(day))
	}
	if by == "" {
		return when
	}
	return by + " " + when
}

// srCalendarDay is toCalendarDateOrNameDay(ts, {shortenMonth}): Today /
// Yesterday, else "Nov 9th" this year and "Nov 9th, 2025" before.
func srCalendarDay(t, now time.Time) string {
	l := i18n.Locale()
	if srSameDay(t, now) || srSameDay(t, now.AddDate(0, 0, -1)) {
		return i18n.RelativeDay(l, t, now, true)
	}
	return i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: t.Year() == now.Year()})
}

func srSameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// srFileActionParts is getActionText (YnzN) for the row's main action as
// Clack opens results (srOpenFile): Open canvas / Open list, "View
// <strong>{fileType}</strong> in Slack" for an image (the viewer),
// "Open in browser" for the rest (their permalink).
func srFileActionParts(f core.File) []i18n.Part {
	switch {
	case f.IsCanvas:
		return []i18n.Part{{Text: nsFiles.T("Open {fileType}", "fileType", "canvas")}}
	case f.IsList:
		return []i18n.Part{{Text: nsFiles.T("Open {fileType}", "fileType", "list")}}
	case f.IsImage:
		kind := f.PrettyType
		if kind == "" {
			kind = strings.ToUpper(f.Filetype)
		}
		return nsFiles.RT("View <strong>{fileType}</strong> in Slack", "fileType", kind)
	}
	return []i18n.Part{{Text: nsFiles.T("Open in browser")}}
}

// srRichLine draws rt parts on one line (<strong> bold), cut at w.
func srRichLine(gtx layout.Context, th *theme.Theme, size, lh float32, col color.NRGBA, parts []i18n.Part, w int) layout.Dimensions {
	x := 0
	lineH := dp(gtx, lh)
	for _, pt := range parts {
		if x >= w || pt.Text == "" {
			continue
		}
		wt := theme.Regular
		if pt.Tag == "strong" || pt.Tag == "b" {
			wt = theme.Bold
		}
		s := pt.Text
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-x), lineH)}
			return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(size), Weight: wt, Color: col, LineHeight: unit.Sp(lh), MaxLines: 1}, s)
		})
		x += d.Size.X
	}
	return layout.Dimensions{Size: image.Pt(min(x, w), lineH)}
}

// ---- Channels ----

// srChanUI is one channel result's extra controls.
type srChanUI struct {
	open, caret, inact widget.Clickable
	tip                TipState
	busy               bool
	// caretR is the caret (view coords of the row, last frame)
	caretR image.Rectangle
}

// srChanBodyH is a channel row inside its top border: padding 16, the
// 22px name row, 4, the 16.25px metadata line, padding 16.
func srChanBodyH(gtx layout.Context) int { return dp(gtx, 16+22+4+16.25+16) }

// srChanRowH is row i of n: the body under its top border, and the last
// row's bottom border.
func srChanRowH(gtx layout.Context, i, n int) int {
	h := border1(gtx) + srChanBodyH(gtx)
	if i == n-1 {
		h += border1(gtx)
	}
	return h
}

// srChanRowY is row i's top in view coordinates (rows above it share its
// height), or false when it's above the list's first row.
func (v *SearchView) srChanRowY(gtx layout.Context, i int) (int, bool) {
	first := v.list.Position.First
	if i < first {
		return 0, false
	}
	return v.listY - v.list.Position.Offset + (i-first)*(border1(gtx)+srChanBodyH(gtx)), true
}

// srChanState is what ChannelSearchResult decides for a channel.
type srChanState struct {
	conv     core.Conv
	known    bool
	general  bool
	canLeave bool
	// inaction is getLeaveChannelReasonContext's label and tooltip ("" for
	// the action buttons)
	inaction, inactionTip string
	temp                  bool // isChannelEligibleForTemporaryJoin (OtvL Y)
	mentions              int
}

func (v *SearchView) chanState(ch core.ChannelHit) srChanState {
	var s srChanState
	c := v.h.Core()
	if c != nil {
		s.conv, s.known = c.Conv(ch.ID)
	}
	s.general = s.known && s.conv.IsGeneral
	shared := s.known && s.conv.IsShared
	member := ch.IsMember
	s.canLeave = member && !s.general
	switch {
	case member && s.general:
		// CANT_LEAVE_GENERAL
		s.inaction, s.inactionTip = nsSearch.T("Required"), nsSearch.T("Your organization requires everyone to be in this channel.")
	case !member && ch.IsArchived:
		s.inaction, s.inactionTip = nsSearch.T("Archived"), nsSearch.T("You can’t join an archived channel, but you can view and search its contents.")
	}
	s.temp = !member && ch.Kind == "channel" && !ch.IsArchived && !s.general && !shared
	if member && !ch.IsArchived && c != nil {
		s.mentions = c.Count(ch.ID).Mentions
	}
	return s
}

// joinLabel is renderJoinOrLeaveText: Leave over a leavable joined
// channel's button, Joined, or Join.
func (s srChanState) joinLabel(member, hover bool) string {
	switch {
	case member && s.canLeave && hover:
		return nsSearch.T("Leave")
	case member:
		return nsSearch.T("Joined")
	}
	return nsSearch.T("Join")
}

// srChanIcon is InlineChannelIcon's glyph (canvasChannelIcon).
func srChanIcon(ch core.ChannelHit) string {
	return canvasChannelIcon(core.Conv{Kind: ch.Kind, IsArchived: ch.IsArchived})
}

// srChanMeta is BrowsePageChannelSubtext's parts: Joined (a member), the
// member count, the purpose on one line.
func srChanMeta(ch core.ChannelHit) []string {
	var out []string
	if ch.IsMember {
		out = append(out, nsSearch.RT("<strong>Joined</strong>")[0].Text)
	}
	if ch.NumMembers >= 0 {
		out = append(out, nsSearch.A("{count, plural, =1 {# member} other {# members}}", i18n.Args{"count": ch.NumMembers}))
	}
	if p := canvasPurposeLine(ch.Purpose); p != "" {
		out = append(out, p)
	}
	return out
}

// srChanButtons is the actions' buttons, right to left: the join button
// (and its caret), then Open in Home.
type srChanButtons struct {
	open, join, caret image.Rectangle // row coords
}

// chanButtons lays out row i's buttons (Slack Kit outline medium) with the
// row's right edge at right.
func (v *SearchView) chanButtons(gtx layout.Context, th *theme.Theme, i, right int, s srChanState, label string) srChanButtons {
	var b srChanButtons
	top := dp(gtx, 12+4)
	h := dp(gtx, 36)
	x := right - dp(gtx, 12+4)
	medW := func(s string) int {
		return max(dp(gtx, 80), textW(gtx, th, 15, theme.Bold, s)+2*dp(gtx, 12)+2*border1(gtx))
	}
	if s.temp {
		cw := dp(gtx, 12+16+12) + border1(gtx)
		b.caret = image.Rect(x-cw, top, x, top+h)
		x -= cw
	}
	jw := medW(label)
	b.join = image.Rect(x-jw, top, x, top+h)
	x -= jw + dp(gtx, 8)
	ow := medW(nsSearch.T("Open in Home"))
	b.open = image.Rect(x-ow, top, x, top+h)
	return b
}

// chanRow draws Channels result i of the page.
func (v *SearchView) chanRow(gtx layout.Context, th *theme.Theme, cl *Colors, i, px, cw int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	ch := v.chans[i]
	ui := &v.chanUI[i]
	n := len(v.chans)
	bw := border1(gtx)
	h := srChanRowH(gtx, i, n)
	s := v.chanState(ch)
	actions := s.inaction == ""
	onButtons := actions && (ui.open.Hovered() || v.btns[i].Hovered() || ui.caret.Hovered())
	hover := v.rows[i].Hovered() || onButtons || ui.inact.Hovered()
	at(gtx, image.Pt(px, 0), func(gtx layout.Context) layout.Dimensions {
		rad := dp(gtx, 8)
		var tr, br int
		if i == 0 {
			tr = rad
		}
		if i == n-1 {
			br = rad
		}
		bg := srTokPryBg.of(th)
		box := image.Rect(0, 0, cw, h)
		if hover {
			softShadow(gtx, box, rad, dp(gtx, 1), dp(gtx, 1), color.NRGBA{A: 0x0f})
		}
		top := theme.Over(srTokFgLow13.of(th), bg)
		rrectCorners(gtx, box, tr, tr, br, br, top)
		in := image.Rect(bw, bw, cw-bw, h)
		if i == n-1 {
			fill(gtx, image.Rect(0, h-bw, cw, h), theme.Over(srTokFgPry10.of(th), bg))
			in.Max.Y -= bw
		}
		fillCol := bg
		if hover {
			fillCol = theme.Over(srTokRowHover.of(th), bg)
		}
		rrectCorners(gtx, in, max(0, tr-bw), max(0, tr-bw), max(0, br-bw), max(0, br-bw), fillCol)
		g := gtx
		g.Constraints = layout.Exact(box.Size())
		v.rows[i].Layout(g, func(gtx layout.Context) layout.Dimensions {
			pointerCursor(gtx, box.Size())
			return layout.Dimensions{Size: box.Size()}
		})
		x0, y0 := bw+dp(gtx, 16), bw+dp(gtx, 16)
		w := cw - 2*x0
		v.chanEntity(gtx, th, cl, ch, s, ui, x0, y0, w, hover && !onButtons)
		if hover && actions {
			label := s.joinLabel(ch.IsMember, v.btns[i].Hovered())
			b := v.chanButtons(gtx, th, i, cw, s, label)
			ui.caretR = b.caret
			at(gtx, b.open.Min, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(b.open.Size())
				return Button(gtx, th, &ui.open, BtnOutline, BtnMedium, nsSearch.T("Open in Home"), "", 0)
			})
			grp := grpNone
			if s.temp {
				grp = grpFirst
			}
			at(gtx, b.join.Min, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(b.join.Size())
				return buttonGrp(gtx, th, &v.btns[i], BtnOutline, BtnMedium, label, "", 0, false, grp)
			})
			if s.temp {
				at(gtx, b.caret.Min, func(gtx layout.Context) layout.Dimensions {
					return v.joinCaret(gtx, th, cl, ui, b.caret.Size())
				})
			}
		} else {
			ui.caretR = image.Rectangle{}
		}
		return layout.Dimensions{Size: box.Size()}
	})
	return layout.Dimensions{Size: image.Pt(px+cw, h)}
}

// joinCaret is the split button's "Join channel temporarily" half: an
// outline medium button without its left border, the caret-down 16.
func (v *SearchView) joinCaret(gtx layout.Context, th *theme.Theme, cl *Colors, ui *srChanUI, sz image.Point) layout.Dimensions {
	return ui.caret.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		pointerCursor(gtx, sz)
		rad := dp(gtx, 8)
		bw := border1(gtx)
		bg := tokBasePry.of(th)
		if ui.caret.Hovered() {
			bg = cl.BtnHover // .c-channel_search_result__action:hover
		}
		line := theme.Over(srTokOtlSec.of(th), tokBasePry.of(th))
		r := image.Rectangle{Max: sz}
		rrectCorners(gtx, r, 0, rad, rad, 0, line)
		rrectCorners(gtx, image.Rect(0, bw, sz.X-bw, sz.Y-bw), 0, max(0, rad-bw), max(0, rad-bw), 0, bg)
		g := dp(gtx, 16)
		at(gtx, image.Pt((sz.X-bw-g)/2, (sz.Y-g)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, icons.Slack("caret-down"), 16, cl.Fg)
		})
		return layout.Dimensions{Size: sz}
	})
}

// chanEntity draws MediumChannelEntity at (x, y), w wide: the name row
// and the metadata line. suffix shows "View channel".
func (v *SearchView) chanEntity(gtx layout.Context, th *theme.Theme, cl *Colors, ch core.ChannelHit, s srChanState, ui *srChanUI, x, y, w int, suffix bool) {
	lineH := dp(gtx, 22)
	nameCol := cl.Fg
	metaCol := tokContentSec.of(th)
	if ch.IsArchived { // c-base_entity--dim
		nameCol = metaCol
	}
	right := x + w
	// the name row's tail, measured first: "View channel" (always laid
	// out), the inaction label, the badge
	vc := nsSearch.T("View channel")
	vcW := textW(gtx, th, 13, theme.Regular, vc)
	tail := dp(gtx, 8) + vcW + dp(gtx, 8)
	inW := 0
	if s.inaction != "" {
		inW = textW(gtx, th, 13, theme.Regular, s.inaction+" ") + dp(gtx, 13)
		tail += dp(gtx, 8) + inW + dp(gtx, 8)
	}
	badgeW := 0
	if s.mentions > 0 {
		badgeW = menuBadgeWidth(gtx, th, s.mentions)
		tail += dp(gtx, 8) + badgeW
	}
	g := dp(gtx, 15)
	ix := g + dp(gtx, 4)
	name := ch.Name
	if ch.IsArchived {
		name += " " + nsSearch.T("(archived)")
	}
	nameMax := max(1, w-ix-dp(gtx, 8)-tail)
	var nd layout.Dimensions
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(0, (lineH-g)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, icons.Slack(srChanIcon(ch)), 15, nameCol)
		})
		nd = at(gtx, image.Pt(ix, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(nameMax, lineH)}
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: nameCol, LineHeight: 22, MaxLines: 1}, name)
		})
		if suffix { // the name's :hover underline rides with the row's
			uy := lineH - dp(gtx, 5)
			one := max(1, dp(gtx, 1)) // the underline's thickness, not a border
			fill(gtx, image.Rect(ix, uy, ix+nd.Size.X, uy+one), nameCol)
		}
		return layout.Dimensions{}
	})
	tx := x + ix + nd.Size.X + dp(gtx, 8)
	small := func(px int, s string, col color.NRGBA) layout.Dimensions {
		return at(gtx, image.Pt(px, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, right-px), lineH)}
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, col, s)
			})
		})
	}
	tx += dp(gtx, 8)
	if suffix {
		small(tx, vc, srTokFgMax70.of(th))
	}
	tx += vcW + dp(gtx, 8)
	if s.inaction != "" {
		tx += dp(gtx, 8)
		lx := tx
		col := srTokFgMax70.of(th)
		hovered := suffix || ui.inact.Hovered()
		sz := image.Pt(inW, lineH)
		at(gtx, image.Pt(lx, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(sz)
			return ui.inact.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if hovered || ui.inact.Hovered() { // opacity 0 until the row is hovered
					vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Regular, col, s.inaction+" ")
					})
					ic := dp(gtx, 13)
					at(gtx, image.Pt(inW-ic, (lineH-ic)/2), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, icons.Slack("info"), 13, col)
					})
				}
				return layout.Dimensions{Size: sz}
			})
		})
		at(gtx, image.Pt(lx, y), func(gtx layout.Context) layout.Dimensions {
			Tooltip(gtx, th, &ui.tip, ui.inact.Hovered(), sz, Above, s.inactionTip, "")
			return layout.Dimensions{}
		})
		tx += inW + dp(gtx, 8)
	}
	if s.mentions > 0 {
		tx += dp(gtx, 8)
		bx := tx + badgeW
		at(gtx, image.Pt(0, y+dp(gtx, 3)), func(gtx layout.Context) layout.Dimensions {
			drawMenuBadge(gtx, th, s.mentions, "red", false, bx, dp(gtx, 18))
			return layout.Dimensions{}
		})
	}
	// metadata
	my := y + lineH + dp(gtx, 4)
	mh := dp(gtx, 16.25)
	at(gtx, image.Pt(x, my), func(gtx layout.Context) layout.Dimensions {
		return srChanMetaLine(gtx, th, ch, metaCol, w, mh)
	})
}

// srChanMetaLine draws the metadata parts on one line, "  ·  " apart,
// with an ellipsis at w.
func srChanMetaLine(gtx layout.Context, th *theme.Theme, ch core.ChannelHit, col color.NRGBA, w, lh int) layout.Dimensions {
	x := 0
	put := func(s string, wt theme.TextStyle) {
		if x >= w {
			return
		}
		d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-x), lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return th.Layout(gtx, wt, s) })
		})
		x += d.Size.X
	}
	style := func(bold bool, c color.NRGBA) theme.TextStyle {
		wt := theme.Regular
		if bold {
			wt = theme.Bold
		}
		return theme.TextStyle{Size: 13, Weight: wt, Color: c, LineHeight: 16.25, MaxLines: 1}
	}
	for i, s := range srChanMeta(ch) {
		if i > 0 {
			put("  ·  ", style(false, col))
		}
		if i == 0 && ch.IsMember {
			// c-medium_channel_entity__joined_status sk_lilypad_green: the
			// check-small glyph at 1em, a space, <strong>Joined</strong>
			green := tokLilypad.of(th)
			g := dp(gtx, 13)
			at(gtx, image.Pt(x, (lh-g)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, icons.Slack("check-small"), 13, green)
			})
			x += g + textW(gtx, th, 13, theme.Regular, " ")
			put(s, style(true, green))
			continue
		}
		put(s, style(false, col))
	}
	return layout.Dimensions{Size: image.Pt(w, lh)}
}

// rrectCorners fills r with per-corner radii (nw, ne, se, sw).
func rrectCorners(gtx layout.Context, r image.Rectangle, nw, ne, se, sw int, c color.NRGBA) {
	if r.Empty() {
		return
	}
	defer clipRRectCorners(gtx, r, nw, ne, se, sw).Pop()
	fill(gtx, r, c)
}

// clipRRectCorners clips to r with per-corner radii.
func clipRRectCorners(gtx layout.Context, r image.Rectangle, nw, ne, se, sw int) clip.Stack {
	return clip.RRect{Rect: r, NW: nw, NE: ne, SE: se, SW: sw}.Push(gtx.Ops)
}

// handleChannels runs the Channels rows' clicks: the row and Open in Home
// open the channel, Join / Leave change the membership, the caret opens
// "Join…".
func (v *SearchView) handleChannels(gtx layout.Context) {
	for i := range v.chans {
		if i >= len(v.chanUI) {
			break
		}
		ch := v.chans[i]
		ui := &v.chanUI[i]
		if v.rows[i].Clicked(gtx) || ui.open.Clicked(gtx) {
			// ChannelListItem's navigate; openChannelResultInHome
			RecordRecentConversation(ch.ID)
			v.h.Navigate("c/" + ch.ID)
		}
		_ = ui.inact.Clicked(gtx) // onInteractionButtonClick: nothing
		if v.btns[i].Clicked(gtx) && !ui.busy {
			s := v.chanState(ch)
			switch {
			case ch.IsMember && s.canLeave:
				v.leaveChannel(i)
			case !ch.IsMember:
				v.joinChannel(i, "")
			}
		}
		if ui.caret.Clicked(gtx) && !ui.busy {
			v.joinMenu(gtx, i)
		}
	}
}

// joinMenu is renderJoinMenu at the caret (position BottomRight).
func (v *SearchView) joinMenu(gtx layout.Context, i int) {
	y, ok := v.srChanRowY(gtx, i)
	ui := &v.chanUI[i]
	if !ok || ui.caretR.Empty() {
		return
	}
	px := v.padX(gtx, gtx.Constraints.Max.X)
	anchor := v.win(gtx, ui.caretR.Add(image.Pt(px, y)))
	join := func(d core.TempJoin) func() { return func() { v.joinChannel(i, d) } }
	items := []MenuItem{
		MenuItem{Header: "Join…"}.In(nsSearch),
		MenuItem{Label: "Just for today", Do: join(core.TempToday)}.In(nsSearch),
		MenuItem{Label: "For 48 hours", Do: join(core.TempTwoDays)}.In(nsSearch),
		MenuItem{Label: "For 1 week", Do: join(core.TempWeek)}.In(nsSearch),
	}
	v.h.ShowOverlay(NewMenuOpts(v.h, items, MenuOpts{Anchor: anchor, Popover: "bottom-right"}))
}

// joinChannel is onClickJoin (joinChannelWithRetryToast, reason
// search_channel_view; d "" for a lasting join): on failure the
// "Couldn’t join {channelName}." toast.
func (v *SearchView) joinChannel(i int, d core.TempJoin) {
	c := v.h.Core()
	if c == nil || i >= len(v.chans) {
		return
	}
	ch := v.chans[i]
	id, seq := ch.ID, v.seq
	v.chanUI[i].busy = true
	run(v.h, func(ctx context.Context) (core.Conv, error) {
		if d != "" {
			return c.JoinFor(ctx, id, d)
		}
		return c.Join(ctx, id)
	}, func(_ core.Conv, err error) {
		if seq != v.seq || i >= len(v.chans) || v.chans[i].ID != id {
			return
		}
		v.chanUI[i].busy = false
		if err != nil {
			v.h.Toast(nsSearch.T("Couldn’t join {channelName}.", "channelName", "#"+ch.Name) + " " +
				nsSearch.T("This could be a connection issue, or just a little blip."))
			return
		}
		v.chans[i].IsMember = true
		v.chans[i].NumMembers++
	})
}

// leaveChannel is onClickLeave: conversations.leave, and on failure the
// "Couldn’t leave {channelName}." toast.
func (v *SearchView) leaveChannel(i int) {
	c := v.h.Core()
	if c == nil || i >= len(v.chans) {
		return
	}
	ch := v.chans[i]
	id, seq := ch.ID, v.seq
	v.chanUI[i].busy = true
	run(v.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.Leave(ctx, id) },
		func(_ struct{}, err error) {
			if seq != v.seq || i >= len(v.chans) || v.chans[i].ID != id {
				return
			}
			v.chanUI[i].busy = false
			if err != nil {
				v.h.Toast(nsSearch.T("Couldn’t leave {channelName}.", "channelName", "#"+ch.Name) + " " +
					nsSearch.T("This could be a connection issue, or just a little blip."))
				return
			}
			v.chans[i].IsMember = false
			if v.chans[i].NumMembers > 0 {
				v.chans[i].NumMembers--
			}
		})
}

// ---- People ----

// srPeopleGrid is the Grid's columns for a w-wide box: auto-fill
// minmax(250px, 1fr) 20 apart, the cell width they stretch to.
func srPeopleGrid(gtx layout.Context, w int) (cols, cell int) {
	gap, minW := dp(gtx, 20), dp(gtx, 250)
	cols = max(1, (w+gap)/(minW+gap))
	cell = (w - gap*(cols-1)) / cols
	return cols, cell
}

// srPersonCardH is UserProfileUnfurl's height: the 96px primary block
// (padding 12, the 72px avatar), the secondary block with a local time
// (1 + 12 + 22 + 12), the banner (8 + 19 + 8: 13px at the unfurl's
// line-height 1.46668).
func srPersonCardH(gtx layout.Context, u core.User) int {
	h := dp(gtx, 96)
	if pfHasClock(u) && !u.IsBot {
		h += border1(gtx) + dp(gtx, 12+22+12)
	}
	if srBannerLabel(u) != "" {
		h += dp(gtx, 35)
	}
	return h
}

// srBannerLabel is MemberProfileHoverBanner's text.
func srBannerLabel(u core.User) string {
	switch {
	case u.IsOwner:
		return i18n.T("team", "Workspace Owner")
	case u.IsAdmin:
		return i18n.T("team", "Workspace Admin")
	case u.Deleted:
		return i18n.T("team", "Deactivated account")
	}
	return ""
}

// srPeopleRowH is grid row j's height: its tallest card, and the 20px gap
// under every row but the last. The first row starts its ring's width
// down (the ring is a box-shadow outside the card, which the list would
// clip).
func (v *SearchView) srPeopleRowH(gtx layout.Context, j, cols int) int {
	h := srPeopleRowTop(gtx, j)
	for i := j * cols; i < min(len(v.people), (j+1)*cols); i++ {
		h = max(h, srPeopleRowTop(gtx, j)+srPersonCardH(gtx, v.people[i]))
	}
	if (j+1)*cols < len(v.people) {
		h += dp(gtx, 20)
	}
	return h
}

func srPeopleRowTop(gtx layout.Context, j int) int {
	if j == 0 {
		return border1(gtx)
	}
	return 0
}

// peopleRow draws grid row j.
func (v *SearchView) peopleRow(gtx layout.Context, th *theme.Theme, cl *Colors, j, px, cw int) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	cols, cell := srPeopleGrid(gtx, cw)
	gap := dp(gtx, 20)
	for k := 0; k < cols; k++ {
		i := j*cols + k
		if i >= len(v.people) {
			break
		}
		x := px + k*(cell+gap)
		at(gtx, image.Pt(x, srPeopleRowTop(gtx, j)), func(gtx layout.Context) layout.Dimensions {
			return v.personCard(gtx, th, cl, i, cell)
		})
	}
	return layout.Dimensions{Size: image.Pt(px+cw, v.srPeopleRowH(gtx, j, cols))}
}

// personCard draws person i's UserProfileUnfurl w wide.
func (v *SearchView) personCard(gtx layout.Context, th *theme.Theme, cl *Colors, i, w int) layout.Dimensions {
	u := v.people[i]
	h := srPersonCardH(gtx, u)
	sz := image.Pt(w, h)
	r := image.Rectangle{Max: sz}
	rad := dp(gtx, 8)
	bg := tokBasePry.of(th)
	cardRing(gtx, r, rad, theme.Over(srTokFgLow13.of(th), bg), bg)
	rrect(gtx, r, rad, bg)
	if v.rows[i].Hovered() {
		rrect(gtx, r, rad, theme.Over(srTokSurfPry.of(th), bg))
	}
	cs := clipRRect(gtx, r, rad)
	defer cs.Pop()
	g := gtx
	g.Constraints = layout.Exact(sz)
	v.rows[i].Layout(g, func(gtx layout.Context) layout.Dimensions {
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
	pad := dp(gtx, 12)
	av := dp(gtx, 72)
	at(gtx, image.Pt(pad, pad), func(gtx layout.Context) layout.Dimensions {
		return avatar(gtx, v.h, pfAvatarURL(u, false), 72)
	})
	colX := pad + av + dp(gtx, 12)
	colW := max(1, w-colX-pad)
	lineH := dp(gtx, 22)
	title := strings.TrimSpace(u.Title)
	pron := strings.TrimSpace(u.Pronouns)
	textH := float32(22)
	if title != "" {
		textH += 22 + 4 // the subtext, then the metadata's margin-top
	}
	if pron != "" {
		textH += 19 // 13px × 1.46668
	}
	y := pad + dp(gtx, (72-(textH-8))/2-4)
	at(gtx, image.Pt(colX, y), func(gtx layout.Context) layout.Dimensions {
		return pfNameLine(gtx, v.h, th, nil, u, 15, 22, 10, colW)
	})
	y += lineH
	if title != "" {
		at(gtx, image.Pt(colX, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, lineH)}
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Regular, Color: srTokFgPry.of(th), LineHeight: 22, MaxLines: 1}, title)
		})
		y += lineH + dp(gtx, 4)
	}
	if pron != "" {
		at(gtx, image.Pt(colX, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, dp(gtx, 19))}
			return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: srTokFgMax70.of(th), LineHeight: 19, MaxLines: 1}, pron)
		})
	}
	by := dp(gtx, 96)
	if pfHasClock(u) && !u.IsBot {
		bw := border1(gtx)
		fill(gtx, image.Rect(0, by, w, by+bw), theme.Over(srTokFgLow13.of(th), bg))
		ly := by + bw + pad
		at(gtx, image.Pt(pad, ly+(lineH-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, icons.Slack("clock"), 20, cl.Fg)
		})
		tx := pad + dp(gtx, 20+8)
		at(gtx, image.Pt(tx, ly), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx-pad), lineH)}
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Regular, Color: cl.Fg, LineHeight: 22, MaxLines: 1},
				pfLocalTimeLabel(v.h, u, time.Now()))
		})
		by = ly + lineH + pad
	}
	if label := srBannerLabel(u); label != "" {
		defer op.Offset(image.Pt(0, by)).Push(gtx.Ops).Pop()
		srBanner(gtx, th, u, label, w)
	}
	return layout.Dimensions{Size: sz}
}

// srBanner is MemberProfileHoverBanner w wide: padding 8 16, the 13px
// bold label (the deactivated one after archive-filled 12, margin-right
// 8); light: sk_foreground_min .04 (deactivated: sk_foreground_low .13 and
// .7 text), dark: sk_foreground_low_solid.
func srBanner(gtx layout.Context, th *theme.Theme, u core.User, label string, w int) {
	fg := srTokFgPry.of(th)
	bg := chipTok{"rgba(var(--sk_foreground_min),0.04)", rgba8(0x1d1c1d, 0x0a), rgba8(0xe8e8e8, 0x0a)}.of(th)
	deact := !u.IsOwner && !u.IsAdmin && u.Deleted
	if deact {
		fg = srTokFgMax70.of(th)
		bg = srTokFgLow13.of(th)
	}
	if th != nil && th.Dark {
		bg = chipTok{"rgb(var(--sk_foreground_low_solid))", rgb(0xdddddd), rgb(0x35373b)}.of(th)
	}
	base := tokBasePry.of(th)
	lh := dp(gtx, 19)
	h := dp(gtx, 8)*2 + lh
	fill(gtx, image.Rect(0, 0, w, h), theme.Over(bg, base))
	x := dp(gtx, 16)
	if deact {
		at(gtx, image.Pt(x, dp(gtx, 8)+(lh-dp(gtx, 12))/2-dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, icons.Slack("archive-filled"), 12, fg)
		})
		x += dp(gtx, 12+8)
	}
	at(gtx, image.Pt(x, dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-x-dp(gtx, 16)), lh)}
		return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Bold, Color: fg, LineHeight: 19, MaxLines: 1}, label)
	})
}

// handlePeople opens a clicked card's member profile (onCellClick:
// navigate to the profile).
func (v *SearchView) handlePeople(gtx layout.Context) {
	for i := range v.people {
		if v.rows[i].Clicked(gtx) {
			pfOpenFull(v.h, v.people[i].ID)
		}
	}
}
