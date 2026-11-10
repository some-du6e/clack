package popups

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The full profile below the buttons: Slack's RimetoProfileSections
// (gantry-v2-async-client-boot-shared module 9xzW, 2026-11-08) over
// users.profile.getSections (core.ProfileSections). Per section, in the
// order the workspace configured them:
//
//   - HEADER: not drawn here (the title and name pronunciation above).
//   - CONTACT (ContactInformationSection): ProfileTextElements only;
//     email/secondEmail as "Email Address" with the email icon and a
//     mailto link, phone/primaryPhone/mobilePhone/"Alternate Phone" with
//     the call icon and a tel link; a missing one the person may edit is
//     "+ Add <label>". Nothing for others' unset fields.
//   - PEOPLE: SCIM manager/report, then each person element: missing →
//     "+ Add <label>", set → its label and the people (avatar, name,
//     title), up to 7 before "Show N more".
//   - the rest (About me = ADDITIONAL_INFO, AFFILIATIONS, custom): Text,
//     Date ("MMM D, YYYY (N years M months ago)"), Link (display text or
//     label), LongText, Tags; missing ones "+ Add <label>".
//
// A section with nothing to show is skipped. Its title is the section
// label (15/900) with "Edit" on the right for yourself when canEdit. The
// frame is .p-r_member_profile_section: a 1px #ddd top border, 16px
// padding, 24 at the bottom of the last one; fields
// .p-rimeto_member_profile_field (8px vertical padding, first and last
// flush): a 13/700 label at 70% and the 15px value.

// pfSecState is the pane's users.profile.getSections result.
type pfSecState struct {
	asked, done bool
	err         error
	ps          core.ProfileSections
	btns        map[string]*widget.Clickable
	more        map[string]bool // People lists expanded past 7
}

func (s *pfSecState) btn(key string) *widget.Clickable {
	if s.btns == nil {
		s.btns = map[string]*widget.Clickable{}
	}
	c := s.btns[key]
	if c == nil {
		c = new(widget.Clickable)
		s.btns[key] = c
	}
	return c
}

// header returns the HEADER section (the name pronunciation lives there).
func (s *pfSecState) header() (core.ProfileSection, bool) {
	if !s.done || s.err != nil {
		return core.ProfileSection{}, false
	}
	return s.ps.Section("HEADER")
}

// pfHeaderData is getHeaderData: the name pronunciation text and whether
// the workspace has the pronunciation and recording fields.
type pfHeaderData struct {
	pronunciation              string
	hasPronunciation, editable bool
	recording                  bool
	recordingFile              string
}

func pfHeaderDataOf(h core.ProfileSection) pfHeaderData {
	var d pfHeaderData
	for _, e := range h.Elements {
		switch {
		case e.ElementKey == "Name Pronunciation" && e.Typename == "ProfileTextElement":
			d.hasPronunciation, d.editable, d.pronunciation = true, e.EditableInUI, e.Text
		case e.ElementKey == "Name Recording" && e.Typename == "ProfileFileElement":
			d.recording, d.recordingFile = true, e.FileID
		}
	}
	return d
}

// fetchSections loads the sections once per pane.
func (v *ProfileView) fetchSections() {
	c := v.h.Core()
	if c == nil || v.secs.asked || v.id == "" {
		return
	}
	v.secs.asked = true
	id, h := v.id, v.h
	run(h, func(ctx context.Context) (core.ProfileSections, error) {
		return c.ProfileSections(ctx, id)
	}, func(ps core.ProfileSections, err error) {
		v.secs.ps, v.secs.err, v.secs.done = ps, err, true
		h.Invalidate()
	})
}

// pfSectionBorder is .p-r_member_profile_section's border-top: #ddd, and
// in the dark theme the #25272b Slack draws (sampled from the desktop app).
func pfSectionBorder(th *theme.Theme) color.NRGBA {
	if th != nil && th.Dark {
		return rgb(0x25272b)
	}
	return rgb(0xdddddd)
}

// sections draws the profile sections from y; returns the new y.
func (v *ProfileView) sections(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, self bool, y, w int) int {
	switch {
	case !v.secs.done:
		return y
	case v.secs.err != nil:
		return v.legacyContact(gtx, th, cl, u, self, y, w)
	}
	return v.drawSections(gtx, th, cl, v.secs.ps.Sections, self, y, w)
}

func (v *ProfileView) drawSections(gtx layout.Context, th *theme.Theme, cl *Colors, secs []core.ProfileSection, self bool, y, w int) int {
	pad := dp(gtx, 16)
	inner := maxInt(1, w-2*pad)
	type drawn struct {
		sec  core.ProfileSection
		rows []pfRow
	}
	var list []drawn
	for _, s := range secs {
		if s.Type == "HEADER" {
			continue
		}
		rows := v.sectionRows(s, self)
		if len(rows) == 0 {
			continue
		}
		list = append(list, drawn{s, rows})
	}
	for i, d := range list {
		fill(gtx, image.Rect(0, y, w, y+1), pfSectionBorder(th))
		y++
		y += pad
		// title bar: label, Edit on the right for yourself
		editW := 0
		if self && d.sec.CanEdit {
			c := v.secs.btn("edit:" + d.sec.ID)
			if c.Clicked(gtx) {
				v.editUntil = gtx.Now.Add(pfEditPaneFor)
				pfEditProfile(v.h)
			}
			editW = textW(gtx, th, 15, theme.Bold, "Edit")
			at(gtx, image.Pt(w-pad-editW, y), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Bold, cl.Link, "Edit")
					})
				})
			})
			editW += dp(gtx, 8)
		}
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, inner-editW), dp(gtx, 22))}
			return para(gtx, th, 15, 22, theme.Black, cl.Fg, d.sec.Label, 1)
		})
		y += dp(gtx, 22) + pad // margin-bottom 16
		for j, r := range d.rows {
			top, bottom := r.padTop, r.padBottom
			if j == 0 {
				top = 0
			}
			if j == len(d.rows)-1 {
				bottom = 0
			}
			y += dp(gtx, top)
			ry := y
			rd := at(gtx, image.Pt(pad, ry), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
				return r.draw(gtx)
			})
			y += rd.Size.Y + dp(gtx, bottom)
		}
		if i == len(list)-1 {
			y += dp(gtx, 24)
		} else {
			y += pad
		}
	}
	return y
}

// pfRow is one field of a section with its CSS vertical padding (dp).
type pfRow struct {
	padTop, padBottom float32
	draw              layout.Widget
}

// sectionRows maps a section's elements to rows (RimetoProfileSections'
// per-type rules; ContactInformationSection for CONTACT).
func (v *ProfileView) sectionRows(s core.ProfileSection, self bool) []pfRow {
	var rows []pfRow
	add := func(top, bottom float32, w layout.Widget) {
		rows = append(rows, pfRow{padTop: top, padBottom: bottom, draw: w})
	}
	missing := func(e core.ProfileElement, top, bottom float32) {
		if !e.EditableInUI || !self {
			return
		}
		add(top, bottom, v.addLink(e))
	}
	switch s.Type {
	case "CONTACT":
		for _, e := range s.Elements {
			if e.Typename != "ProfileTextElement" {
				continue
			}
			if e.IsMissing {
				missing(e, 4, 4) // the wrapper's padding_top_50 / padding_bottom_50
				continue
			}
			if strings.TrimSpace(e.Text) == "" {
				continue
			}
			switch e.ElementKey {
			case "phone", "primaryPhone", "mobilePhone", "Alternate Phone":
				add(8, 8, v.contactRow(e, "phone", "tel:"))
			case "email", "secondEmail":
				add(8, 8, v.contactRow(e, "mail", "mailto:"))
			}
		}
	case "PEOPLE":
		for _, key := range []string{"manager", "report"} {
			for _, e := range s.Elements {
				if e.ElementKey == key && e.IsScimManaged && len(e.Persons) > 0 {
					add(10, 6, v.peopleRows(e))
				}
			}
		}
		for _, e := range s.Elements {
			if e.IsScimManaged {
				continue
			}
			if e.Type == "PERSON" && e.IsMissing {
				missing(e, 8, 8)
			}
			if e.Typename == "ProfilePersonElement" && len(e.Persons) > 0 {
				add(10, 6, v.peopleRows(e))
			}
		}
	default:
		for _, e := range s.Elements {
			if e.IsHidden {
				continue
			}
			switch e.Typename {
			case "ProfileTextElement", "ProfileDateElement", "ProfileLinkElement", "ProfileLongTextElement", "ProfileTagsElement":
			default:
				continue
			}
			if e.IsMissing {
				missing(e, 8, 8)
				continue
			}
			if w := v.valueRow(e); w != nil {
				add(8, 8, w)
			}
		}
	}
	return rows
}

// pfField draws a 13/700 field label at 70% over the value.
func pfField(gtx layout.Context, th *theme.Theme, label string, value layout.Widget) layout.Dimensions {
	w := gtx.Constraints.Max.X
	y := 0
	if label != "" {
		d := at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 18))}
			return para(gtx, th, 13, 18, theme.Bold, pfFieldLabel(th), label, 1)
		})
		y += maxInt(d.Size.Y, dp(gtx, 18))
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return value(gtx)
	})
	return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y)}
}

// addLink is MissingElement's AddLink: the plus icon, a space and
// "Add <label>" in the link colour; it opens the profile editor.
func (v *ProfileView) addLink(e core.ProfileElement) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := v.h.Theme()
		cl := colorsOf(th)
		c := v.secs.btn("add:" + e.ID)
		if c.Clicked(gtx) {
			v.editUntil = gtx.Now.Add(pfEditPaneFor)
			pfEditProfile(v.h)
		}
		return pfLinkRow(gtx, th, cl, c, "Add "+e.Label, dp(gtx, 22))
	}
}

// contactRow is a contact field: the 20px icon in a 36x34 tile
// (#1d1c1d0a, radius 4, margin-right 8), the label and the value as a
// mailto/tel link.
func (v *ProfileView) contactRow(e core.ProfileElement, ic, scheme string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := v.h.Theme()
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		tw, labelH, valueH := dp(gtx, 36), dp(gtx, 18), dp(gtx, 22)
		tile := image.Rect(0, 0, tw, dp(gtx, 34))
		textH := labelH + valueH
		h := maxInt(tile.Dy(), textH)
		at(gtx, image.Pt(0, (h-tile.Dy())/2), func(gtx layout.Context) layout.Dimensions {
			rrect(gtx, tile, dp(gtx, 4), theme.WithAlpha(cl.Fg, 0x0a))
			at(gtx, image.Pt(dp(gtx, 8), dp(gtx, 7)), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, ic, 20, cl.Fg)
			})
			return layout.Dimensions{Size: tile.Max}
		})
		x := tw + dp(gtx, 8)
		c := v.secs.btn("contact:" + e.ID)
		if c.Clicked(gtx) {
			if err := openurl.Open(scheme + e.Text); err != nil {
				v.h.Toast("Couldn't open that: " + errText(err))
			}
		}
		at(gtx, image.Pt(x, (h-textH)/2), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), textH)}
			return pfField(gtx, th, e.Label, func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 15, 22, theme.Regular, cl.Link, e.Text, 1)
				})
			})
		})
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
}

// valueRow draws a set Text/Date/Link/LongText/Tags element (nil when it
// has nothing to show).
func (v *ProfileView) valueRow(e core.ProfileElement) layout.Widget {
	var value string
	var link func()
	switch e.Typename {
	case "ProfileTextElement", "ProfileLongTextElement":
		value = e.Text
		if e.IsFilterable && e.LegacyFieldID != "" {
			// Slack: a people search filtered by this field value
			link = func() { v.h.Toast("Searching people by profile field isn't supported yet") }
		}
	case "ProfileDateElement":
		value = pfProfileDate(e.Date, time.Now())
	case "ProfileLinkElement":
		value = e.DisplayText
		if value == "" {
			value = e.Label
		}
		uri := strings.TrimSpace(e.URI)
		link = func() {
			if uri == "" {
				return
			}
			if err := openurl.Open(uri); err != nil {
				v.h.Toast("Couldn't open that link: " + errText(err))
			}
		}
	case "ProfileTagsElement":
		value = strings.Join(e.Tags, "   ")
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return func(gtx layout.Context) layout.Dimensions {
		th := v.h.Theme()
		cl := colorsOf(th)
		return pfField(gtx, th, e.Label, func(gtx layout.Context) layout.Dimensions {
			if link == nil {
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, value, 0)
			}
			c := v.secs.btn("value:" + e.ID)
			if c.Clicked(gtx) {
				link()
			}
			return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.Link, value, 0)
			})
		})
	}
}

// peopleRows is RimetoProfilePeopleSection: the label, then each person
// (36px avatar, bold name, 13px title), 12 apart, opening their profile;
// past 7 a "Show N more" link.
func (v *ProfileView) peopleRows(e core.ProfileElement) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := v.h.Theme()
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		y := 0
		if e.Label != "" {
			at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 18))}
				return para(gtx, th, 13, 18, theme.Bold, pfFieldLabel(th), e.Label, 1)
			})
			y += dp(gtx, 18) + dp(gtx, 10)
		}
		ids := e.Persons
		const trunc = 7
		open := v.secs.more[e.ID]
		if !open && len(ids) > trunc {
			ids = ids[:trunc]
		}
		av := dp(gtx, 36)
		for i, id := range ids {
			if i > 0 {
				y += dp(gtx, 12)
			}
			p, ok := lookupUser(v.h, id)
			c := v.secs.btn("person:" + e.ID + ":" + id)
			if c.Clicked(gtx) {
				pfOpenFull(v.h, id)
			}
			py := y
			at(gtx, image.Pt(0, py), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
					if ok {
						pfImage(gtx, v.h, pfAvatarURL(p, false), 36, 6)
					} else {
						rrect(gtx, image.Rect(0, 0, av, av), dp(gtx, 6), cl.Selected)
					}
					x := av + dp(gtx, 8)
					tw := maxInt(1, w-x)
					name := id
					if ok {
						name = pfName(p)
					}
					title := ""
					if ok {
						title = p.Title
					}
					ny := 0
					if title == "" {
						ny = (av - dp(gtx, 22)) / 2
					}
					at(gtx, image.Pt(x, ny), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 22))}
						return para(gtx, th, 15, 22, theme.Bold, cl.Fg, name, 1)
					})
					if title != "" {
						at(gtx, image.Pt(x, dp(gtx, 20)), func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 18))}
							return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, title, 1)
						})
					}
					return layout.Dimensions{Size: image.Pt(w, av)}
				})
			})
			y += av
		}
		if len(e.Persons) > trunc {
			y += dp(gtx, 4)
			label := "Show less"
			if !open {
				label = "Show " + strconv.Itoa(len(e.Persons)-trunc) + " more"
			}
			c := v.secs.btn("more:" + e.ID)
			if c.Clicked(gtx) {
				if v.secs.more == nil {
					v.secs.more = map[string]bool{}
				}
				v.secs.more[e.ID] = !open
			}
			d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Link, label)
					})
				})
			})
			y += d.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(w, y)}
	}
}

// legacyContact is the pane without users.profile.getSections (Slack's
// RimetoProfileLegacyData): the email Slack shows you, nothing invented.
func (v *ProfileView) legacyContact(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, self bool, y, w int) int {
	if pfBlank(u.Email) {
		return y
	}
	s := core.ProfileSection{ID: "legacy-contact", Label: "Contact information", Type: "CONTACT", CanEdit: self,
		Elements: []core.ProfileElement{{ID: "legacy-email", Label: "Email Address", Typename: "ProfileTextElement", ElementKey: "email", Text: u.Email}}}
	return v.drawSections(gtx, th, cl, []core.ProfileSection{s}, self, y, w)
}

// pfProfileDate is DateElement's value: "MMM D, YYYY" and the tenure,
// " (N years M months ago)", or " (N days ago)" under a month; nothing
// for future dates.
func pfProfileDate(date string, now time.Time) string {
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(firstN(date, 10)), now.Location())
	if err != nil {
		return date
	}
	out := d.Format("Jan 2, 2006")
	if d.After(now) {
		return out
	}
	months := (now.Year()-d.Year())*12 + int(now.Month()) - int(d.Month())
	if now.Day() < d.Day() {
		months--
	}
	years, months := months/12, months%12
	var parts []string
	switch {
	case years == 1:
		parts = append(parts, "1 year")
	case years > 1:
		parts = append(parts, strconv.Itoa(years)+" years")
	}
	switch {
	case months == 1:
		parts = append(parts, "1 month")
	case months > 1:
		parts = append(parts, strconv.Itoa(months)+" months")
	}
	if len(parts) > 0 {
		return out + " (" + strings.Join(parts, " ") + " ago)"
	}
	days := int(now.Sub(d).Hours() / 24)
	switch {
	case days < 1:
		return out
	case days == 1:
		return out + " (a day ago)"
	}
	return out + " (" + strconv.Itoa(days) + " days ago)"
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
