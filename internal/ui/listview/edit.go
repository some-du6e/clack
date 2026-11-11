package listview

import (
	"encoding/json"
	"errors"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ctl is what the list view and the item pane share: the host, the list,
// the shell's actions and the cell being edited.
// exp: the viewer is in the experiment's "on" group (getGroupForUser;
// docs/EXPERIMENTS.md, Lists).
func (c *ctl) exp(name string) bool {
	a := c.h.Core()
	return a != nil && a.Experiment(name) == "on"
}

type ctl struct {
	h    Host
	id   string
	act  Actions
	edit cellEdit
	// lastNewText is the name the last new item was given ("" when its
	// editor closed empty).
	lastNewText string
}

// cellEdit is the inline editor (text, number, email, phone, link, date).
type cellEdit struct {
	active   bool
	rec      string // "" = a new item
	col      *lists.Column
	ed       widget.Editor
	focus    bool
	focused  bool
	groupCol *lists.Column // a new item's group (board lane / table group)
	group    string
	original string
	// done hears how the editor closed (doneEnter, doneEscape, doneBlur);
	// the grid moves its active cell with it.
	done func(how int)
	// rich is the rich editor of a formatted text cell (richedit.go; nil:
	// the plain editor), richOrig the cell's rich_text when it opened.
	rich     *composer.Editor
	richOrig json.RawMessage
}

// errViewOnly is the reason an edit is refused when the viewer can't edit.
var errViewOnly = errors.New("view only")

// canEdit is the fidelity guard plus the file's own permission.
func canEdit(d *core.ListDoc, c *lists.Column, f *lists.Field) error {
	if d == nil || !d.Info.Editable {
		return errViewOnly
	}
	return lists.Editable(c, f)
}

// refuse explains a refused edit the way Clack does for canvases: a toast,
// and nothing is sent. Computed fields and view-only lists stay silent,
// as in Slack.
func (k *ctl) refuse(err error) {
	var u lists.Unsupported
	if errors.As(err, &u) {
		k.h.Toast("This field can't be edited in Clack yet (" + u.Error() + ")")
	}
}

// cellClick starts the edit Slack starts on a click in a cell: toggles
// for checkboxes and votes, a menu for selects and people, the inline
// editor for typed values, a direct pick for ratings (x is the click's
// offset in the cell).
func (k *ctl) cellClick(d *core.ListDoc, rec *lists.Record, c *lists.Column, x int, gtx layout.Context) {
	k.cellClickWith(d, rec, c, x, gtx, nil)
}

// cellClickWith is cellClick with the menus shown by show (nil: the
// shell's menu at the pointer); nextRow tells show whether a pick saves
// with nextRowOnSave (single selects).
func (k *ctl) cellClickWith(d *core.ListDoc, rec *lists.Record, c *lists.Column, x int, gtx layout.Context, show func(items []MenuItem, nextRow bool)) {
	if show == nil {
		show = func(items []MenuItem, _ bool) {
			if k.act.Menu != nil {
				k.act.Menu(items)
			}
		}
	}
	if rec == nil || c == nil {
		return
	}
	f := rec.Field(c.Key)
	if err := canEdit(d, c, f); err != nil {
		if !k.startRich(d, rec, c, f, err, "") { // richedit.go
			k.refuse(err)
		}
		return
	}
	self := ""
	if a := k.h.Core(); a != nil {
		self = a.Self().ID
	}
	e := lists.Edit{RowID: rec.ID, Column: c}
	switch c.Type {
	case lists.Checkbox, lists.Completed, lists.TodoCompleted:
		e.Check = !(f != nil && f.Checkbox != nil && *f.Checkbox)
		k.save(e)
	case lists.Vote:
		var us []string
		had := false
		if f != nil {
			for _, u := range f.User {
				if u == self {
					had = true
					continue
				}
				us = append(us, u)
			}
		}
		if !had {
			us = append(us, self)
		}
		e.Values = us
		k.save(e)
	case lists.Rating:
		n := x/gtx.Dp(26) + 1
		if n > c.Options.Max {
			n = c.Options.Max
		}
		if f != nil && len(f.Rating) > 0 && int(f.Rating[0]) == n {
			e.Clear = true
		} else {
			e.Rating = n
		}
		k.save(e)
	case lists.Select, lists.MultiSelect:
		show(k.selectItems(rec, c, f), !c.Multi())
	case lists.User, lists.Assignee, lists.TodoAssignee:
		if items := k.peopleItems(d, rec, c, f); items != nil {
			show(items, false)
		}
	case lists.Channel:
		if items := k.channelItems(d, rec, c, f); items != nil {
			show(items, false)
		}
	default:
		k.edit.start(rec.ID, c, editText(k.h, c, f))
	}
}

// editText is a cell's value as the inline editor shows it.
func editText(h Host, c *lists.Column, f *lists.Field) string {
	if f == nil {
		return ""
	}
	switch c.Type {
	case lists.Number, lists.Currency:
		if len(f.Number) > 0 {
			return strconv.FormatFloat(f.Number[0], 'f', -1, 64)
		}
		return ""
	case lists.Link:
		if len(f.Link) > 0 {
			return f.Link[0].OriginalURL
		}
		return ""
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		if len(f.Date) > 0 {
			return f.Date[0]
		}
		return ""
	}
	return CellText(h.Core(), c, f)
}

func (e *cellEdit) start(rec string, c *lists.Column, text string) {
	e.active, e.rec, e.col, e.focus, e.focused = true, rec, c, true, false
	e.rich, e.richOrig = nil, nil
	e.ed.SingleLine = true
	e.ed.Submit = true
	e.ed.SetText(text)
	e.ed.SetCaret(len([]rune(text)), len([]rune(text)))
	e.original = text
	e.groupCol, e.group = nil, ""
	e.done = nil
}

// startNew opens the editor on a new item's primary field.
func (k *ctl) startNew(d *core.ListDoc, groupCol *lists.Column, group string) {
	if d == nil || !d.Info.Editable {
		return
	}
	p := d.Meta.Primary()
	if p == nil {
		return
	}
	k.edit.start("", p, "")
	k.edit.groupCol, k.edit.group = groupCol, group
}

// editing reports whether the inline editor is on rec's column c.
func (e *cellEdit) editing(rec string, c *lists.Column) bool {
	return e.active && e.rec == rec && e.col != nil && c != nil && e.col.Key == c.Key
}

// layout draws the inline editor in a w×h box (Slack's cell in edit mode:
// a 2px lagoon border) and commits on Enter or when focus leaves; Escape
// cancels.
func (k *ctl) layoutEditor(gtx layout.Context, d *core.ListDoc, w, h int) layout.Dimensions {
	e := &k.edit
	if e.rich != nil {
		return k.layoutRich(gtx, d, w, h) // richedit.go
	}
	th := k.h.Theme()
	c := &th.C
	shortcut.OwnEscape(&e.ed)
	finish := func(how int) layout.Dimensions {
		done := e.done
		e.done = nil
		if how == doneEscape {
			e.active = false
		} else {
			k.commit(d)
		}
		if done != nil {
			done(how)
		}
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	for {
		ev, ok := e.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			return finish(doneEnter)
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &e.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		if kv, ok := ev.(key.Event); ok && kv.State == key.Press {
			return finish(doneEscape)
		}
	}
	if e.focus {
		gtx.Execute(key.FocusCmd{Tag: &e.ed})
		e.focus = false
	} else if gtx.Focused(&e.ed) {
		e.focused = true
	} else if e.focused {
		return finish(doneBlur) // clicked elsewhere
	}
	r := image.Rect(0, 0, w, h)
	theme.Fill(gtx, r, c.Bg)
	theme.Border(gtx, r, gtx.Dp(2), gtx.Dp(2), shade(th, "lagoon-50"))
	pad := gtx.Dp(10)
	at(gtx, image.Pt(pad, (h-gtx.Dp(22))/2), image.Pt(max(0, w-2*pad), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		if e.ed.Len() == 0 {
			ph := placeholder(e.col, e.rec == "")
			th.Label(gtx, 15, theme.Regular, c.FgSubtle, ph)
		}
		return th.Editor(gtx, &e.ed, 15, c.Fg, c.Selection)
	})
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func placeholder(c *lists.Column, isNew bool) string {
	if isNew {
		return "Name this item"
	}
	switch c.Type {
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		return "YYYY-MM-DD"
	case lists.Link:
		return "https://"
	case lists.Number, lists.Currency:
		return "0"
	}
	return ""
}

// commit turns the editor's text into an edit and saves it.
func (k *ctl) commit(d *core.ListDoc) {
	e := &k.edit
	if !e.active {
		return
	}
	if e.rich != nil {
		k.commitRich() // richedit.go
		return
	}
	e.active = false
	txt := strings.TrimSpace(e.ed.Text())
	c := e.col
	if e.rec == "" { // a new item
		k.lastNewText = txt
		if txt == "" {
			return
		}
		fields := []lists.Edit{{Column: c, Text: txt}}
		if e.groupCol != nil && e.group != "" && lists.Editable(e.groupCol, nil) == nil {
			fields = append(fields, groupEdit(e.groupCol, e.group))
		}
		k.create(fields)
		return
	}
	if txt == e.original {
		return
	}
	ed := lists.Edit{RowID: e.rec, Column: c, Clear: txt == ""}
	switch c.Type {
	case lists.Number, lists.Currency:
		if txt != "" {
			n, err := strconv.ParseFloat(strings.ReplaceAll(txt, ",", ""), 64)
			if err != nil {
				k.h.Toast("Enter a number")
				return
			}
			ed.Number = &n
		}
	case lists.Link:
		if txt != "" {
			if !strings.Contains(txt, "://") {
				txt = "https://" + txt
			}
			// the typed link (I6yI d: {originalUrl, displayName, attachment:
			// null}, so display_as_url is false); unchanged when the cell's
			// first link already has this URL
			ed.Link = &lists.LinkValue{OriginalURL: txt}
			if d != nil {
				if r := d.Record(e.rec); r != nil {
					if f := r.Field(c.Key); f != nil && len(f.Link) > 0 && f.Link[0].OriginalURL == txt {
						return // unchanged
					}
				}
			}
		}
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		if txt != "" {
			t, err := parseDay(txt, c)
			if err != nil {
				k.h.Toast("Enter a date like " + time.Now().Format("2006-01-02"))
				return
			}
			ed.Values = []string{t.Format("2006-01-02")}
		}
	default:
		ed.Text = txt
	}
	k.save(ed)
}

func parseDay(s string, c *lists.Column) (time.Time, error) {
	for _, lay := range []string{"2006-01-02", momentLayout(c.Options.DateFormat), "Jan 2, 2006", "1/2/2006", "2 Jan 2006"} {
		if t, err := time.ParseInLocation(lay, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("not a date")
}

func groupEdit(c *lists.Column, v string) lists.Edit {
	e := lists.Edit{Column: c}
	switch c.Type {
	case lists.Checkbox, lists.Completed, lists.TodoCompleted:
		e.Check = v == "true"
	default:
		e.Values = []string{v}
	}
	return e
}

// save writes one cell off the UI goroutine; a failure tells the user and
// reloads the list.
func (k *ctl) save(e lists.Edit) {
	a := k.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		if err := a.UpdateListCells(ctx, k.id, e); err != nil {
			k.h.Toast("Couldn't save the change: " + err.Error())
			rc, rcancel := ctxTimeout(30 * time.Second)
			_, _ = a.List(rc, k.id)
			rcancel()
		}
		k.h.Invalidate()
	}()
}

func (k *ctl) create(fields []lists.Edit) {
	a := k.h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := ctxTimeout(30 * time.Second)
		defer cancel()
		if _, err := a.CreateListRecord(ctx, k.id, "", "", fields...); err != nil {
			k.h.Toast("Couldn't add the item: " + err.Error())
		}
		k.h.Invalidate()
	}()
}

// selectItems is a select field's options; multi-selects toggle.
func (k *ctl) selectItems(rec *lists.Record, c *lists.Column, f *lists.Field) []MenuItem {
	cur := map[string]bool{}
	var vals []string
	if f != nil {
		for _, s := range f.Select {
			cur[s] = true
			vals = append(vals, s)
		}
	}
	multi := c.Multi()
	var items []MenuItem
	for _, ch := range c.Options.Choices {
		ch := ch
		icon := ""
		if cur[ch.Value] {
			icon = "check"
		}
		items = append(items, MenuItem{Label: ch.Label, Icon: icon, Do: func() {
			e := lists.Edit{RowID: rec.ID, Column: c}
			switch {
			case multi && cur[ch.Value]:
				for _, v := range vals {
					if v != ch.Value {
						e.Values = append(e.Values, v)
					}
				}
				e.Clear = len(e.Values) == 0
			case multi:
				e.Values = append(append([]string(nil), vals...), ch.Value)
			case cur[ch.Value]:
				return
			default:
				e.Values = []string{ch.Value}
			}
			k.save(e)
		}})
	}
	if len(vals) > 0 {
		items = append(items, MenuItem{Label: "Clear", Icon: "close", Do: func() { k.save(lists.Edit{RowID: rec.ID, Column: c, Clear: true}) }})
	}
	return items
}

// peopleItems offers the viewer, the people already in the list and the
// shell's suggestions; multi-person fields toggle.
func (k *ctl) peopleItems(d *core.ListDoc, rec *lists.Record, c *lists.Column, f *lists.Field) []MenuItem {
	a := k.h.Core()
	if a == nil {
		return nil
	}
	cur := map[string]bool{}
	var vals []string
	if f != nil {
		for _, u := range f.User {
			cur[u] = true
			vals = append(vals, u)
		}
	}
	seen := map[string]bool{}
	var ids []string
	add := func(u string) {
		if u != "" && !seen[u] && strings.HasPrefix(u, "U") || u != "" && !seen[u] && strings.HasPrefix(u, "W") {
			seen[u] = true
			ids = append(ids, u)
		}
	}
	add(a.Self().ID)
	for _, u := range vals {
		add(u)
	}
	var others []string
	for _, r := range d.Records {
		for _, fl := range r.Fields {
			if col := d.Meta.Column(fl.Key); col != nil && (col.Type == lists.User || col.Type == lists.TodoAssignee || col.Type == lists.Assignee) {
				others = append(others, fl.User...)
			}
		}
	}
	if k.act.People != nil {
		others = append(others, k.act.People()...)
	}
	sort.Strings(others)
	for _, u := range others {
		if len(ids) >= 12 {
			break
		}
		add(u)
	}
	multi := c.Multi()
	var items []MenuItem
	for _, u := range ids {
		u := u
		name := u
		if us, ok := a.User(u); ok {
			name = us.Name
		}
		if u == a.Self().ID {
			name += " (you)"
		}
		icon := "user"
		if cur[u] {
			icon = "check"
		}
		items = append(items, MenuItem{Label: name, Icon: icon, Do: func() {
			e := lists.Edit{RowID: rec.ID, Column: c}
			switch {
			case multi && cur[u]:
				for _, v := range vals {
					if v != u {
						e.Values = append(e.Values, v)
					}
				}
				e.Clear = len(e.Values) == 0
			case multi:
				e.Values = append(append([]string(nil), vals...), u)
			case cur[u]:
				return
			default:
				e.Values = []string{u}
			}
			k.save(e)
		}})
	}
	if len(vals) > 0 {
		items = append(items, MenuItem{Label: "Remove", Icon: "close", Do: func() { k.save(lists.Edit{RowID: rec.ID, Column: c, Clear: true}) }})
	}
	return items
}

// channelItems offers the channels the list already mentions.
func (k *ctl) channelItems(d *core.ListDoc, rec *lists.Record, c *lists.Column, f *lists.Field) []MenuItem {
	a := k.h.Core()
	if a == nil {
		return nil
	}
	cur := map[string]bool{}
	if f != nil {
		for _, ch := range f.Channel {
			cur[ch] = true
		}
	}
	seen := map[string]bool{}
	var items []MenuItem
	for _, r := range d.Records {
		if fl := r.Field(c.Key); fl != nil {
			for _, ch := range fl.Channel {
				if seen[ch] {
					continue
				}
				seen[ch] = true
				ch := ch
				name := ch
				if cv, ok := a.Conv(ch); ok {
					name = "#" + cv.Name
				}
				icon := "hash"
				if cur[ch] {
					icon = "check"
				}
				items = append(items, MenuItem{Label: name, Icon: icon, Do: func() {
					k.save(lists.Edit{RowID: rec.ID, Column: c, Values: []string{ch}})
				}})
			}
		}
	}
	if len(cur) > 0 {
		items = append(items, MenuItem{Label: "Clear", Icon: "close", Do: func() { k.save(lists.Edit{RowID: rec.ID, Column: c, Clear: true}) }})
	}
	if len(items) == 0 {
		items = append(items, MenuItem{Label: "No channels to pick from", Disabled: true})
	}
	return items
}
