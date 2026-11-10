package views

// Group DM details, from Slack's about-channel-modal (vWNA, build
// 4d0c78085232):
//
//   - the header is MPDMInfo: MPDMTitle (boot-shared m6yC, not
//     truncated). A named group DM (core.MpdmCustomName) shows its name
//     after BaseChannelPrefixMpim (popups.MpimPrefix, 16px); otherwise the
//     other members' names sorted (3gAC Z), all of them up to three, else
//     "A, B, N others". The title drops to 18px
//     (.p-view_header__channel_title--small) when the conversation's own
//     name, the custom one or the internal mpdm-… one, has 30 characters
//     or more.
//   - About opens with MpdmConversationNameField when the viewer may name
//     the group DM (core.CanNameMpdm: mpdm_naming, sc_mpdm_naming for
//     Slack Connect, not a frontline worker): "Conversation name", the
//     custom name or "Add a name" (sk_foreground_max), and an Edit
//     ("Edit conversation name") that opens RenameMpdmDialog stacked over
//     the details (entrypoint channel_details).
//   - Topic and Description are EditableFields with isDisabled when the
//     viewer can't set them (core.CanSetTopic / CanSetPurpose: xjwB and
//     uJL7 over permissions/info): no Edit, no hover, the text at 70%
//     (.p-field--disabled: rgba(--sk_foreground_max, .7)).

import (
	"image"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// mpdmOthers are the group DM's members other than the viewer, sorted by
// their names (3gAC Z), with those names.
func (d *ChannelDetails) mpdmOthers(members []string) (ids, names []string) {
	self := d.selfID()
	type m struct{ id, name string }
	var ms []m
	for _, id := range members {
		if id == self {
			continue
		}
		ms = append(ms, m{id, d.userName(id)})
	}
	sort.SliceStable(ms, func(i, j int) bool { return strings.ToLower(ms[i].name) < strings.ToLower(ms[j].name) })
	for _, x := range ms {
		ids, names = append(ids, x.id), append(names, x.name)
	}
	return ids, names
}

// cdMpdmList is MPDMTitle's member text.
func cdMpdmList(names []string) string {
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= 3:
		return strings.Join(names, ", ")
	}
	n := len(names) - 2
	if n == 1 {
		return names[0] + ", " + names[1] + ", 1 other"
	}
	return names[0] + ", " + names[1] + ", " + strconv.Itoa(n) + " others"
}

// mpdmTitle is MPDMInfo's title: the text, whether it is a custom name,
// and whether it is --small.
func (d *ChannelDetails) mpdmTitle(cv core.Conv) (title string, named, small bool) {
	c := d.h.Core()
	if n, ok := c.MpdmCustomName(d.id); ok {
		return n, true, utf8.RuneCountInString(n) >= 30
	}
	raw := cv.Name
	if sc, ok := c.S.Conv(d.id); ok {
		raw = sc.Name
	}
	_, names := d.mpdmOthers(cv.Members)
	title = cdMpdmList(names)
	if title == "" {
		title = mpimName(cv.Name)
	}
	return title, false, utf8.RuneCountInString(raw) >= 30
}

// mpdmPrefix draws the named group DM's BaseChannelPrefixMpim.
func (d *ChannelDetails) mpdmPrefix(gtx C, cv core.Conv) D {
	ids, _ := d.mpdmOthers(cv.Members)
	var av layout.Widget
	if len(ids) > 0 {
		if u, ok := d.user(ids[0]); ok {
			url := dirFirst(u.Avatar, u.Avatar192, u.Avatar512)
			av = func(gtx C) D {
				s := gtx.Constraints.Max.X
				return dirImage(&d.base, gtx, url, image.Pt(s, s), gtx.Dp(4))
			}
		}
	}
	return popups.MpimPrefix(gtx, d.th, 16, max(len(cv.Members)-1, 2), av)
}

// mpdmNameField is MpdmConversationNameField.
func (d *ChannelDetails) mpdmNameField(gtx C) D {
	name := ""
	if n, ok := d.h.Core().MpdmCustomName(d.id); ok {
		name = n
	}
	return d.cdField(gtx, "Conversation name", &d.x.mpdmName, func(gtx C) D {
		if name == "" {
			return d.para(gtx, 15, 22, wReg, d.p.Muted, 0, "Add a name")
		}
		return d.para(gtx, 15, 22, wReg, d.p.Text, 0, name)
	})
}

// openRenameMpdm opens RenameMpdmDialog over the details (isStackable).
func (d *ChannelDetails) openRenameMpdm(cv core.Conv) {
	ph, ok := d.h.(popups.Host)
	if !ok {
		return
	}
	_, names := d.mpdmOthers(cv.Members)
	ph.ShowOverlay(popups.NewRenameMpdmDialog(ph, d.id, strings.Join(names, ", ")))
}

// cdFieldDisabled is a disabled EditableField: the title and text at 70%,
// no Edit.
func (d *ChannelDetails) cdFieldDisabled(gtx C, label string, value W) D {
	defer paint.PushOpacity(gtx.Ops, 0.7).Pop()
	return d.cdField(gtx, label, nil, value)
}

// ---- test hooks ----

// TabLabels are the tab strip's labels, in order.
func (d *ChannelDetails) TabLabels() []string {
	out := make([]string, len(d.tabKinds))
	for i := range d.tabKinds {
		out[i] = d.tab.Labels[i]
	}
	return out
}

// AboutFields are the About tab's editable-field titles for the loaded
// conversation (Conversation name, Topic, Description), each with whether
// it offers Edit.
func (d *ChannelDetails) AboutFields() map[string]bool {
	out := map[string]bool{}
	c := d.h.Core()
	if cdGroup(d.kind) == "mpim" && c.CanNameMpdm(d.id) {
		out["Conversation name"] = true
	}
	out["Topic"] = c.CanSetTopic(d.id)
	if g := cdGroup(d.kind); g != "im" && g != "mpim" {
		out["Description"] = c.CanSetPurpose(d.id)
	}
	return out
}

// Title is the header's title text.
func (d *ChannelDetails) Title() string {
	inf, _, _ := d.info.get()
	t, _ := d.title(d.conv(inf, true))
	return t
}
