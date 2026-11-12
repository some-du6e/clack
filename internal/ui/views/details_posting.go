package views

// Settings › Posting permissions (gantry-v2 build d3de3a487b1c,
// 2026-11-12; chunk shared-…admin-channel-modal_tsx, module oWlM; the
// rules and calls are core/chposting.go):
//
//   - ViewPostingPermissions: the first row of the "permissions" section,
//     an EditableField "Posting permissions" (aria "Edit posting
//     permissions"); its text is PostingPermissionsSubtitle, three bullets
//     (.p-about_modal__posting_permissions_subtitle: margin 4px 0 8px
//     20px) — who can post, who can reply, the @channel line — then
//     "Learn more" (articleId 0x53d1f2cb9f). Disabled, it has no Edit and
//     no help icon.
//   - PostingPermissionsModal (stacked): "Channel permissions" on Plus or
//     higher, else "Posting permissions". PostingPreferences loads the
//     four prefs (a jumbo spinner until then), then PostingPermissions
//     draws "Who can post in this channel?" and its radios
//     (formatPermissionSettings), the member picker for "…plus specific
//     people" (padding_left_150, "Select people…"), "Allow threads" when
//     not everyone can post, and the mentions checkbox (disabled and off
//     past loud_channel_mentions_limit for a non-admin). Cancel / Save
//     Changes (disabled until something changed; setHasChanges compares
//     the last change with the loaded value). A failed save shows its
//     error alert at the top.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// PostingPermissions' I18n namespace (ViewPostingPermissions and
// PostingPermissionsModal use nsChannelDetails, details_call.go).
var (
	nsChanOptions = i18n.NS("channel_options") // PostingPermissions, createPermissionsSubtitleBulleted
)

// Help articles the posting permissions link to.
const (
	cdPostingHelpArticle  = "https://slack.com/help/articles/360004635551" // the subtitle's Learn more (0x53d1f2cb9f)
	cdMentionsHelpArticle = "https://slack.com/help/articles/202009646"    // "how mentions work" (0xc0a6c2e)
)

// cdPosting is PostingPermissionsModal's state.
type cdPosting struct {
	prefs     async[core.PostingPrefs]
	form      core.PostingForm
	orig      core.PostingForm // the loaded values (PostingPreferences' refs)
	seeded    bool
	changed   bool // setHasChanges
	hereLock  bool // disableCanAtHereAtChannel
	radios    map[string]*widget.Clickable
	thread    widget.Clickable
	here      widget.Clickable
	learn     widget.Clickable
	pick      cdPeoplePick
	saveOp    async[bool]
	saving    bool
	err       string
	viewer    core.PostingViewer
	saveTried bool
}

// postingViewer is PostingViewer for this channel.
func (d *ChannelDetails) postingViewer(inf core.ChannelDetails) core.PostingViewer {
	c := d.h.Core()
	_, admin, _ := d.selfRoles()
	perms, _, _ := d.x.perms.get()
	v := core.PostingViewer{SelfID: d.selfID(), WorkspaceAdmin: admin, ManagePerms: perms[core.PermManageChannelPermission],
		OrgShared: inf.IsOrgShared}
	if c != nil && c.C != nil {
		v.Enterprise = c.C.Creds.EnterpriseID != ""
		if u, ok := c.S.User(d.selfID()); ok && u.EnterpriseUser != nil {
			v.EnterpriseAdmin = u.EnterpriseUser.IsAdmin
		}
	}
	return v
}

// postingVisibility is additionalOptionsToShow's postingPermissions.
func (d *ChannelDetails) postingVisibility(inf core.ChannelDetails) core.PostingVisibility {
	c := d.h.Core()
	if c == nil || cdGroup(d.kind) != "channel" {
		return core.PostingVisibility{}
	}
	cv := d.conv(inf, true)
	v := d.postingViewer(inf)
	perms, _, _ := d.x.perms.get()
	return core.PostingPermsVisibility(core.PostingVisIn{
		Channel:         true,
		ExtSharedHosted: inf.IsExtShared && d.hostedElsewhere(inf),
		General:         cv.IsGeneral,
		Member:          inf.IsMember,
		FeatureAccess:   c.FeatureAccess(core.FeatureManageChannelPosting),
		ManagePerms:     perms[core.PermManageChannelPermission],
		ManagePosting:   perms[core.PermManageChannelPosting],
		EnterpriseAdmin: v.EnterpriseAdmin,
		WorkspaceAdmin:  v.WorkspaceAdmin || v.EnterpriseAdmin, // canUserActAsWorkspaceAdmin
		CanPost:         c.PostingIn(d.id).CanPost(),
		StandardPlan:    c.OnStandardPlan(),
		UpsellTreatment: c.Experiment("boost_bus_plus_awareness_and_upgrades") == "treatment",
	})
}

// loadPostingSubtitle starts (or, after a save, repeats) the subtitle's
// four reads.
func (d *ChannelDetails) loadPostingSubtitle(again bool) {
	if d.x.postSub.started() && !again {
		return
	}
	c, id := d.h.Core(), d.id
	d.x.postSub.load(d.h, true, func(ctx context.Context) (core.PostingPrefs, error) {
		return c.ChannelPostingPrefs(ctx, id), nil
	})
}

// postingField is ViewPostingPermissions.
func (d *ChannelDetails) postingField(gtx C, inf core.ChannelDetails, vis core.PostingVisibility) D {
	d.loadPostingSubtitle(false)
	if d.x.postRow.Clicked(gtx) && !vis.Disabled {
		d.openPosting(inf)
	}
	if d.x.postLearn.Clicked(gtx) {
		hostHandler{d.h}.OpenURL(cdPostingHelpArticle)
	}
	fg := d.p.Text
	if vis.Disabled {
		fg = cdDim(fg)
	}
	return d.cdFieldOf(gtx, &d.x.postRow, cdFieldSpec{titleText: nsChannelDetails.T("Posting permissions"), disabled: vis.Disabled,
		text: func(gtx C) D { return d.postingSubtitle(gtx, inf, fg) }})
}

// postingSubtitle is PostingPermissionsSubtitle: the bullets (nothing
// until who_can_post has a value), then "Learn more".
func (d *ChannelDetails) postingSubtitle(gtx C, inf core.ChannelDetails, fg color.NRGBA) D {
	p, loaded, _ := d.x.postSub.get()
	if !loaded {
		return D{}
	}
	who, thread, here, ok := core.PostingSubtitle(p, d.postingViewer(inf))
	lines := cdPostingBullets(who, thread, here, d.conv(inf, true).IsGeneral, d.postingExternal(inf), d.postingTeamName())
	return cdStack2(gtx,
		func(gtx C) D {
			if !ok || len(lines) == 0 {
				return D{}
			}
			return layout.Inset{Top: 4, Bottom: 8, Left: 20}.Layout(gtx, func(gtx C) D {
				ch := make([]layout.FlexChild, 0, len(lines))
				for _, l := range lines {
					l := l
					ch = append(ch, layout.Rigid(func(gtx C) D {
						return row(gtx, layout.Start,
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, fg, 1, "•") })
							}),
							layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, fg, 0, l) }),
						)
					}))
				}
				return col(gtx, ch...)
			})
		},
		func(gtx C) D {
			return cdLinkReg(&d.base, gtx, &d.x.postLearn, 15, i18n.Plain(nsChannelDetails.RT("<link>Learn more</link>")))
		},
	)
}

// cdStack2 stacks two widgets.
func cdStack2(gtx C, a, b W) D {
	return col(gtx, layout.Rigid(a), layout.Rigid(b))
}

// cdPostingBullets is createPermissionsSubtitleBulleted's three lines
// ("" who: none).
func cdPostingBullets(who string, thread, here, general, external bool, team string) []string {
	if who == "" {
		return nil
	}
	ns := nsChanOptions
	var first string
	switch who {
	case "ra":
		first = ns.T("Everyone can post")
	case "regular":
		first = ns.T("Everyone except guests can post")
		if external {
			first = ns.T("Only {teamName} members can post", "teamName", team)
		}
	case "admin":
		first = ns.T("Only admins can post")
		if external {
			first = ns.T("Only {teamName} admins can post", "teamName", team)
		}
	case "org_admin":
		first = ns.T("Only Org Admins can post")
		if external {
			first = ns.T("Only {teamName} Org Admins can post", "teamName", team)
		}
	case "admin_multi":
		first = ns.T("Only admins and specific people can post")
		if external {
			first = ns.T("Only {teamName} admins and specific people can post", "teamName", team)
		}
	case "owner_multi":
		first = ns.T("Only Org Owners and specific people can post")
		if external {
			first = ns.T("Only {teamName} Org Owners and specific people can post", "teamName", team)
		}
	case "org_admin_multi":
		first = ns.T("Only Org Admins and specific people can post")
		if external {
			first = ns.T("Only {teamName} Org Admins and specific people can post", "teamName", team)
		}
	default:
		first = ns.T("Only specific people can post")
	}
	second := ns.T("No one can reply to messages")
	if thread {
		second = ns.T("Everyone can reply to messages")
	}
	third := ns.T("No one can use @channel or @here mentions")
	if here {
		m := ns.T("@channel and @here")
		if general {
			m = ns.T("@everyone")
		}
		third = ns.T("Because of your workspace settings, only people who have permission can use {mentionsAndText} mentions", "mentionsAndText", m)
	}
	return []string{first, second, third}
}

// postingExternal is shouldShowExternalChannelCopy: an externally shared
// channel this org hosts.
func (d *ChannelDetails) postingExternal(inf core.ChannelDetails) bool {
	return inf.IsExtShared && !d.hostedElsewhere(inf)
}

// postingTeamName is the TeamName of the top-level team (the org on Grid).
func (d *ChannelDetails) postingTeamName() string {
	c := d.h.Core()
	if c == nil || c.C == nil {
		return ""
	}
	id := c.C.Creds.EnterpriseID
	if id == "" {
		id = c.C.Creds.Team
	}
	if n, _, ok := c.Team(id, 0); ok && n != "" {
		return n
	}
	return c.S.TeamName
}

// ---- PostingPermissionsModal ----

func (d *ChannelDetails) openPosting(inf core.ChannelDetails) {
	d.openDialog(cdDlgPosting)
	c, id := d.h.Core(), d.id
	p := &cdPosting{radios: map[string]*widget.Clickable{}, viewer: d.postingViewer(inf)}
	p.pick.init(nsChanOptions.T("Select people…"))
	p.prefs.load(d.h, false, func(ctx context.Context) (core.PostingPrefs, error) { return c.ChannelPostingPrefs(ctx, id), nil })
	d.x.dlg.post = p
}

// postingOptions is formatPermissionSettings: the radios, in order.
func (d *ChannelDetails) postingOptions(inf core.ChannelDetails, p *cdPosting) []cdOption {
	c := d.h.Core()
	ns := nsChanOptions
	ext, team := d.postingExternal(inf), d.postingTeamName()
	v := p.viewer
	grid := v.Enterprise && inf.IsOrgShared
	can := v.ManagePerms
	if grid {
		can = can || v.EnterpriseAdmin
	} else {
		can = can || v.WorkspaceAdmin || v.EnterpriseAdmin // canUserActAsWorkspaceAdmin
	}
	guest := false
	if u, ok := c.User(d.selfID()); ok {
		guest = u.Guest
	}
	e := cdOption{"ra", ns.T("Everyone"), true}
	if ext {
		e.label = ns.T("Everyone in the channel")
	}
	regular := ns.T("Everyone, except guests")
	if c.Experiment("frontline_users") == "on" && c.FeatureAccess(core.FeatureFrontline) {
		regular = ns.T("Everyone, except frontline members and guests")
	}
	if ext {
		regular = ns.T("Only members from {teamName}", "teamName", team)
	}
	t := &cdOption{"regular", regular, true}
	s := &cdOption{"org_admin", ns.T("Org Admins/Owners"), false}
	if ext {
		s.label = ns.T("Only Org Admins/Owners from {teamName}", "teamName", team)
	}
	i := cdOption{"multi", ns.T("Org Admins and Owners, plus select members"), false}
	if ext {
		i.label = ns.T("{teamName} Org Admins and Owners, plus specific people", "teamName", team)
	}
	if grid {
		s.label = ns.T("Org Admins only")
		i.label = ns.T("Org Admins, plus specific people")
		if ext {
			s.label = ns.T("Only Org Admins from {teamName}", "teamName", team)
			i.label = ns.T("{teamName} Org Admins, plus specific people", "teamName", team)
		}
	} else {
		s.who, s.label = "admin", ns.T("Admins only")
		i.label = ns.T("Admins, plus specific people")
		if ext {
			s.label = ns.T("Only admins from {teamName}", "teamName", team)
			i.label = ns.T("{teamName} admins, plus specific people", "teamName", team)
		}
	}
	if !can {
		s = nil
		i.label = ns.T("You, and specific people")
		if guest {
			t = nil
		}
	}
	out := []cdOption{e}
	if t != nil {
		out = append(out, *t)
	}
	if s != nil {
		out = append(out, *s)
	}
	return append(out, i)
}

// cdOption is one radio.
type cdOption struct {
	who, label  string
	hideOrgDflt bool // hideWhenOrgDefault
}

func (d *ChannelDetails) updatePosting(gtx C, inf core.ChannelDetails) {
	dl := &d.x.dlg
	p := dl.post
	if p == nil {
		return
	}
	c, id := d.h.Core(), d.id
	if !p.seeded {
		if prefs, done, _ := p.prefs.get(); done && !p.prefs.busy() {
			if f := core.PostingFormState(prefs, p.viewer); f.OK {
				p.form, p.orig, p.seeded = f, f, true
				p.form.Users = append([]string(nil), f.Users...)
				p.pick.ids = append([]string(nil), f.Users...)
				if c.PostingAtChannelLocked(id) {
					p.form.AtHere, p.hereLock = false, true
				}
			}
		}
	}
	if dl.close.Clicked(gtx) {
		d.closeDialog()
		return
	}
	if p.learn.Clicked(gtx) {
		hostHandler{d.h}.OpenURL(cdMentionsHelpArticle)
	}
	if p.seeded && !p.saving {
		for who, r := range p.radios {
			if r.Clicked(gtx) {
				p.form.Who = who
				p.changed = p.orig.Who != who
				if who == "multi" {
					p.pick.focus = true
				}
			}
		}
		if p.thread.Clicked(gtx) {
			p.form.CanThread = !p.form.CanThread
			p.changed = p.orig.CanThread != p.form.CanThread
		}
		if p.here.Clicked(gtx) && !p.hereLock {
			p.form.AtHere = !p.form.AtHere
			p.changed = p.orig.AtHere != p.form.AtHere
		}
		if p.form.Who == "multi" && p.pick.update(gtx, d, c) {
			p.form.Users = append([]string(nil), p.pick.ids...)
			p.changed = true // onChangeValueSelectedMembers always marks a change
		}
	}
	if dl.ok.Clicked(gtx) && p.seeded && p.changed && !p.saving {
		prefs := core.PostingSavePrefs(p.form.Who, p.form.Users, p.form.CanThread, p.form.AtHere, p.viewer)
		p.saving, p.err, p.saveTried = true, "", true
		p.saveOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.SetChannelPrefs(ctx, id, prefs) })
	}
	if p.saving && !p.saveOp.busy() {
		if _, done, err := p.saveOp.get(); done {
			p.saving = false
			if err != nil {
				var se *slack.Error
				if errors.As(err, &se) && se.Code == "restricted_action" {
					p.err = nsChannelDetails.T("Unable to save posting permissions. It seems like the posting permission for this channel has been changed and you can no longer change this preference.")
				} else {
					p.err = nsChannelDetails.T("Oops, something went wrong with updating message posting permissions. Please try again.")
				}
				return
			}
			d.closeDialog()
			d.loadPostingSubtitle(true)
		}
	}
}

// postingDialog draws PostingPermissionsModal.
func (d *ChannelDetails) postingDialog(gtx C, full image.Point, inf core.ChannelDetails) {
	dl := &d.x.dlg
	p := dl.post
	if p == nil {
		return
	}
	c := d.h.Core()
	title := nsChannelDetails.T("Posting permissions")
	if c.OnPlusPlanOrHigher() {
		title = nsChannelDetails.T("Channel permissions")
	}
	var body []W
	if p.err != "" {
		body = append(body, d.cdAlert(p.err, false), cdSpace(16))
	}
	if !p.seeded {
		body = append(body, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Top: 40, Bottom: 40}.Layout(gtx, func(gtx C) D {
				return layout.Center.Layout(gtx, func(gtx C) D { return actSpinner(gtx, gtx.Dp(32), d.p.Muted) }) // jumbo
			})
		})
	} else {
		ns := nsChanOptions
		body = append(body, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 0, ns.T("Who can post in this channel?")) }, cdSpace(8))
		for _, o := range d.postingOptions(inf, p) {
			o := o
			if o.hideOrgDflt && p.viewer.OrgDefault {
				continue
			}
			body = append(body, func(gtx C) D {
				return layout.Inset{Bottom: 6}.Layout(gtx, func(gtx C) D {
					r := dirClick(p.radios, o.who)
					return r.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return cdRadio(&d.base, gtx, p.form.Who == o.who) }),
							hgap(8),
							layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, o.label) }),
						)
					})
				})
			})
		}
		if p.form.Who == "multi" {
			body = append(body, func(gtx C) D {
				return layout.Inset{Left: 24, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D { return p.pick.layout(gtx, d) })
			})
		}
		general := d.conv(inf, true).IsGeneral
		if p.form.Who != "ra" {
			body = append(body, cdSpace(16), func(gtx C) D { return hline(gtx, d.p.Border) }, cdSpace(16),
				d.cdCheckRow(&p.thread, p.form.CanThread, false, ns.T("Allow threads"),
					func(gtx C) D {
						return d.para(gtx, 15, 22, wReg, d.p.Muted, 0, ns.T("Everyone will be able to add replies to messages posted in this channel, regardless of overall posting permissions"))
					}))
		} else {
			body = append(body, cdSpace(16), func(gtx C) D { return hline(gtx, d.p.Border) }, cdSpace(16))
		}
		label := ns.T("Allow @here and @channel mentions")
		desc := ns.RT("<strong>@channel</strong> notifies all members of a channel, and <strong>@here</strong> notifies members of a channel who are active at that moment. Learn more about <Link>how mentions work</Link>.")
		if c.Experiment("enable_new_channel_settings") == "on" {
			desc = ns.RT("These mentions notify everyone in the channel. Learn more about <Link>how mentions work</Link>.")
		}
		if general {
			label = ns.T("Allow @everyone, @here and @channel mentions")
			desc = ns.RT("<strong>@everyone</strong> notifies every person in the #general channel, <strong>@channel</strong> notifies all members of a channel, and <strong>@here</strong> notifies members of a channel who are active at that moment. Learn more about <Link>how mentions work</Link>.")
		}
		body = append(body, d.cdCheckRow(&p.here, p.form.AtHere, p.hereLock, label, func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D { return d.cdRichParts(gtx, "post-mentions-"+label, desc, cdMentionsHelpArticle) }),
				layout.Rigid(func(gtx C) D {
					if c.Experiment("enable_new_channel_settings") == "on" {
						return layout.Inset{Top: 16}.Layout(gtx, d.cdAlert(ns.T("Only members with permission can use @here and @channel in this workspace"), true))
					}
					return d.cdRichParts(gtx, "post-note", ns.RT("<strong> Note:</strong> Because of your workspace settings, only people who have permission can use <strong>@here</strong> and <strong>@channel</strong>."), "")
				}),
			)
		}))
	}
	d.skModal(gtx, full, title, true, body, []W{
		d.cdDlgButton(&dl.cancel, btnOutline, nsChannelDetails.T("Cancel"), false),
		d.cdDlgButton(&dl.ok, btnPrimary, nsChannelDetails.T("Save Changes"), !p.seeded || !p.changed || p.saving),
	})
}

// cdCheckRow is an inline c-label with a checkbox: the box, the 15/22
// text, the subtext under it.
func (d *ChannelDetails) cdCheckRow(c *widget.Clickable, on, disabled bool, text string, sub W) W {
	return func(gtx C) D {
		draw := func(gtx C) D {
			return row(gtx, layout.Start,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 3, Right: 8}.Layout(gtx, func(gtx C) D { return cdCheckbox(&d.base, gtx, on, disabled) })
				}),
				layout.Flexed(1, func(gtx C) D {
					fg := d.p.Text
					if disabled {
						fg = cdDim(fg)
					}
					return cdStack2(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, fg, 0, text) }, sub)
				}),
			)
		}
		if disabled {
			return draw(gtx)
		}
		return c.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			return draw(gtx)
		})
	}
}

// cdRichParts draws rt() parts as a muted 15/22 paragraph: <strong> and
// <b> bold, <Link>/<a> a link to url.
func (d *ChannelDetails) cdRichParts(gtx C, key string, parts []i18n.Part, url string) D {
	spans := make([]richtext.Span, 0, len(parts))
	var plain strings.Builder
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		plain.WriteString(p.Text)
		s := richtext.Span{Kind: richtext.Text, Text: p.Text}
		switch strings.ToLower(p.Tag) {
		case "strong", "b":
			s.Style = richtext.Bold
		case "link", "a":
			if url != "" {
				s.Kind, s.URL = richtext.Link, url
			}
		}
		spans = append(spans, s)
	}
	doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
	return d.richDoc(gtx, "dlg/"+key+"/"+d.id, plain.String(), doc, richview.Options{Secondary: true}, 0)
}

// ---- the member picker (SearchSelect, multi) ----

// cdPeoplePick is the "…plus specific people" picker: the picked people
// as tokens (the viewer never shown), the input, matches in a menu under
// it.
type cdPeoplePick struct {
	q      *search
	deb    debounce
	res    async[[]core.User]
	ids    []string
	tokClk map[string]*widget.Clickable
	resClk map[string]*widget.Clickable
	focus  bool
}

func (k *cdPeoplePick) init(placeholder string) {
	k.q = newSearch(placeholder)
	k.tokClk, k.resClk = map[string]*widget.Clickable{}, map[string]*widget.Clickable{}
}

// update runs the input, the search and the token clicks; true when the
// picked people changed.
func (k *cdPeoplePick) update(gtx C, d *ChannelDetails, c *core.App) bool {
	if k.q == nil {
		return false
	}
	if k.focus {
		k.focus = false
		gtx.Execute(key.FocusCmd{Tag: &k.q.ed})
	}
	q, changed, _ := k.q.update(gtx)
	if changed {
		k.deb.set(q, gtx.Now)
	}
	if dq, due := k.deb.due(gtx, dirDebounce); due && dq != "" {
		k.res.load(d.h, true, func(ctx context.Context) ([]core.User, error) { return c.SearchUsers(ctx, dq, 10) })
	}
	moved := false
	for uid, t := range k.tokClk {
		if t.Clicked(gtx) {
			for i, id := range k.ids {
				if id == uid {
					k.ids = append(k.ids[:i:i], k.ids[i+1:]...)
					moved = true
					break
				}
			}
		}
	}
	for uid, r := range k.resClk {
		if r.Clicked(gtx) {
			if !strContains(k.ids, uid) {
				k.ids = append(k.ids, uid)
				moved = true
			}
			k.q.ed.SetText("")
			k.q.last = ""
			k.res = async[[]core.User]{}
			gtx.Execute(key.FocusCmd{Tag: &k.q.ed})
		}
	}
	return moved
}

func strContains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// layout draws the field; the menu is deferred above the dialog.
func (k *cdPeoplePick) layout(gtx C, d *ChannelDetails) D {
	w := gtx.Constraints.Max.X
	self := d.selfID()
	c := d.h.Core()
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 6, Bottom: 6, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var ch []layout.FlexChild
		for _, id := range k.ids {
			if id == self {
				continue // Slack's picker leaves the viewer out of its tokens
			}
			id := id
			u, _ := c.User(id)
			t := dirClick(k.tokClk, id)
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 4, Top: 2, Bottom: 2}.Layout(gtx, func(gtx C) D {
					return t.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						mm := op.Record(gtx.Ops)
						td := layout.Inset{Top: 2, Bottom: 2, Left: 4, Right: 6}.Layout(gtx, func(gtx C) D {
							return row(gtx, layout.Middle,
								layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 20) }),
								hgap(6),
								layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, firstNonEmpty(displayName(u), id)) }),
								hgap(4),
								layout.Rigid(func(gtx C) D { return d.icon(gtx, "close", 12, d.p.Sec) }),
							)
						})
						call := mm.Stop()
						rrect(gtx, image.Rectangle{Max: td.Size}, gtx.Dp(4), flatten(cdTileBg, d.p.Surface))
						call.Add(gtx.Ops)
						return td
					})
				})
			}))
		}
		ch = append(ch, layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min.Y = gtx.Dp(28)
			return layout.W.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return d.th.Editor(gtx, &k.q.ed, 15, d.p.Text, d.p.Mention)
			})
		}))
		return row(gtx, layout.Middle, ch...)
	})
	call := m.Stop()
	r := image.Rect(0, 0, w, max(dims.Size.Y, gtx.Dp(40)))
	rad := gtx.Dp(8)
	focused := gtx.Focused(&k.q.ed)
	if focused {
		ring := gtx.Dp(5)
		rrect(gtx, r.Inset(-ring), rad+ring, flatten(theme.WithAlpha(d.p.Link, 0x4d), d.p.Surface))
		one := max(1, gtx.Dp(1))
		rrect(gtx, r.Inset(-one), rad+one, d.p.Link)
	}
	rrect(gtx, r, rad, d.p.Surface)
	if !focused {
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.BorderStr)
	}
	call.Add(gtx.Ops)
	shown := 0
	for _, id := range k.ids {
		if id != self {
			shown++
		}
	}
	if k.q.text() == "" && shown == 0 {
		dirAt(gtx, image.Pt(gtx.Dp(8), (r.Dy()-gtx.Sp(22))/2), func(gtx C) D { return d.label(gtx, 15, wReg, d.p.Faint, k.q.placeholder()) })
	}
	if k.q.text() != "" {
		mm := op.Record(gtx.Ops)
		op.Offset(image.Pt(0, r.Dy()+gtx.Dp(4))).Add(gtx.Ops)
		k.results(gtx, d, w)
		op.Defer(gtx.Ops, mm.Stop())
	}
	return D{Size: r.Size()}
}

// results is the select's menu: up to 10 people (no deactivated
// accounts; workflows allowed, as searcherOptions' includeWorkflows),
// with avatars.
func (k *cdPeoplePick) results(gtx C, d *ChannelDetails, w int) {
	res, loaded, err := k.res.get()
	var us []core.User
	for _, u := range res {
		if u.Deleted || u.ID == "USLACKBOT" {
			continue
		}
		us = append(us, u)
		if len(us) == 10 {
			break
		}
	}
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Dp(320))}
	dims := layout.Inset{Top: 12, Bottom: 12}.Layout(g, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if !loaded || err != nil || len(us) == 0 {
			msg := "No matches found"
			if !loaded || k.res.busy() {
				msg = "Searching…"
			}
			return layout.Inset{Left: 24, Right: 24, Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Muted, 1, msg) })
		}
		ch := make([]layout.FlexChild, 0, len(us))
		for _, u := range us {
			u := d.viewUser(u)
			c := dirClick(k.resClk, u.ID)
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return d.hoverable(gtx, c, false, 0, func(gtx C) D {
					return layout.Inset{Top: 4, Bottom: 4, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 20) }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, displayName(u)) }),
						)
					})
				})
			}))
		}
		return col(gtx, ch...)
	})
	call := m.Stop()
	r := image.Rectangle{Max: dims.Size}
	rad := gtx.Dp(8)
	theme.Shadow(gtx, r, rad, gtx.Dp(12), argb(0x26000000))
	one := max(1, gtx.Dp(1))
	rrect(gtx, r.Inset(-one), rad+one, dirRing(&d.base, d.p.Bg))
	rrect(gtx, r, rad, d.p.Surface)
	func() {
		defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
}

// ---- test hooks (the ui package's shots) ----

// PostingSelect picks a radio of the open Posting permissions modal (ra,
// regular, admin, org_admin, multi), as clicking it does.
func (d *ChannelDetails) PostingSelect(who string) {
	if p := d.x.dlg.post; p != nil {
		dirClick(p.radios, who).Click()
		d.h.Invalidate()
	}
}

// PostingLoaded reports that the open modal's form has its values.
func (d *ChannelDetails) PostingLoaded() bool { return d.x.dlg.post != nil && d.x.dlg.post.seeded }
