package views

// Directories: Slack's People / Channels / User Groups browser (spec:
// /tmp/ui-views-spec/directories.md). This file also holds helpers that
// details.go shares (dirClick, dirPress, dirDropdown, dirTabStrip,
// dirSearchField, dirFill, dirImage, dirPresence, dirRowShape); they are
// prefixed to stay clear of the other views in the package.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const (
	dirTabPeople = iota
	dirTabChannels
	dirTabGroups
	dirTabExternal
	dirTabInvites
)

const (
	dirPad      = 35  // content inset each side
	dirCardMin  = 180 // grid: repeat(auto-fill, minmax(180px, 1fr))
	dirCardGap  = 16
	dirCardText = 100 // info block under the card's square photo
	dirDebounce = 250 * time.Millisecond
)

var (
	dirTabLabels = []string{"People", "Channels", "User Groups", "External", "Invitations"}
	dirTabIcons  = []string{"user", "hash", "people", "globe", "send"}

	dirPlaceholders = [3]string{"Search for people", "Search for channels", "Search by team name, project or department"}
	dirActions      = [3]string{"Invite People", "Create Channel", "Create user group"}

	dirSortLabels = [3][]string{
		{"Most recommended", "A to Z", "Z to A"},
		{"Most recommended", "A to Z", "Most members", "Newest"},
		{"A to Z", "Z to A", "Most members", "Newest"},
	}
	// dirChanSorts maps the channel sort menu to core.ChannelBrowseOpts
	// (Sort, Desc).
	dirChanSorts = []struct {
		sort string
		desc bool
	}{{"", false}, {"name", false}, {"member_count", true}, {"created", true}}
	// dirGroupSorts maps the user group sort menu to core.UsergroupOpts.
	dirGroupSorts = []struct {
		sort string
		desc bool
	}{{"name", false}, {"name", true}, {"member_count", true}, {"date_create", true}}

	dirHeroTitle = [3]string{"Invite your team to Slack", "", "Assemble your dream team"}
	dirHeroBody  = [3]string{
		"Bring your team members into Slack to start working better together. Send invites via email, or get a handy link to share.",
		"",
		"Easily @mention departments or teams all at once by bundling them into user groups.",
	}
	dirHeroBtn = [3]string{"Invite people", "", "Create a user group"}

	dirActive = rgb(0x20a271) // presence: active
)

type dirFilter struct {
	icon, label string
	caret       bool
}

var dirFilters = [3][]dirFilter{
	{{"", "Title", true}, {"", "Location", true}, {"", "Workspaces", true}, {"", "Department", true}, {"filter", "More", true}},
	{{"", "All channels", true}, {"", "Any channel type", true}, {"", "Workspaces", true}, {"", "Organizations", true}},
	{{"", "Workspaces", true}, {"", "Exclude deactivated", false}},
}

// dirChans is the channel browser's accumulated result pages.
type dirChans struct {
	list               []core.ChannelHit
	total, page, pages int
	q                  string
}

// dirJoinOp is an optimistic Join/Leave in flight. prev is the membership
// shown before the click, restored if the write fails.
type dirJoinOp struct {
	id, name   string
	join, prev bool
	a          async[bool]
}

// dirMemberNote is a membership fact learned off the UI goroutine
// (realtime events, verification reads). seq is the channel's op sequence
// when the read started; a note older than the latest own write is dropped.
type dirMemberNote struct {
	id     string
	member bool
	seq    int
	ev     bool // straight from an event (applies unless a write is in flight)
	verify bool // a "conv" event: verify membership with a read
}

// dirEvents queues realtime notes for the UI goroutine. start subscribes
// once (UI goroutine only); stop unsubscribes.
type dirEvents struct {
	mu     sync.Mutex
	q      []dirMemberNote
	on     bool
	cancel func()
}

func (e *dirEvents) start(h Host, match func(ev core.Event) (dirMemberNote, bool)) {
	if e.on {
		return
	}
	e.on = true
	ch, cancel := h.Core().Subscribe()
	e.cancel = cancel
	go func() {
		for ev := range ch {
			if n, ok := match(ev); ok {
				e.push(h, n)
			}
		}
	}()
}

func (e *dirEvents) push(h Host, n dirMemberNote) {
	e.mu.Lock()
	if len(e.q) < 512 {
		e.q = append(e.q, n)
	}
	e.mu.Unlock()
	h.Invalidate()
}

func (e *dirEvents) drain() []dirMemberNote {
	e.mu.Lock()
	defer e.mu.Unlock()
	q := e.q
	e.q = nil
	return q
}

func (e *dirEvents) stop() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

// dirPeople is the people browser's accumulated result pages.
type dirPeople struct {
	list               []core.User
	total, page, pages int
	q                  string
}

// Directories is the Directories view.
type Directories struct {
	exclDisabled bool // User Groups "Exclude deactivated"
	base
	tab       *tabs
	srch      [3]*search
	deb       [3]debounce
	sorts     [3]dirDropdown
	press     dirPress
	retry     widget.Clickable
	clear     widget.Clickable
	action    [3]widget.Clickable
	heroClose [3]widget.Clickable
	heroBtn   [3]widget.Clickable
	heroOff   [3]bool
	fclk      map[string]*widget.Clickable
	wantFocus int // frames left to try focusing the search box

	people  async[dirPeople]
	peopleQ string
	peopleL widget.List
	cards   map[string]*widget.Clickable
	acct    dirDropdown // People "More": account type

	chans   async[dirChans]
	chanL   widget.List
	rows    map[string]*widget.Clickable
	joinBtn map[string]*widget.Clickable
	caret   map[string]*widget.Clickable
	ops     []*dirJoinOp
	// member overrides the loaded hits' membership with what this client
	// knows better: its own Join/Leave and realtime conv events. It
	// survives reloads (search results can lag behind a join).
	member  map[string]bool
	opSeq   map[string]int
	evq     dirEvents
	confirm string // private channel awaiting the Leave confirmation
	confYes widget.Clickable
	confNo  widget.Clickable
	menuID  string // channel whose Join/Leave caret menu is open
	rowMenu dirDropdown
	purp    map[string]string // plainPurpose cache
	// Channel filters (server-side, core.ChannelBrowseOpts): membership
	// scope and channel type.
	chanScope dirDropdown
	chanType  dirDropdown

	groups  async[[]core.Usergroup]
	groupsQ string
	groupL  widget.List
}

// NewDirectories builds the Directories view (People tab selected).
func NewDirectories(h Host) *Directories {
	d := &Directories{
		base:      newBase(h),
		tab:       newTabs(dirTabLabels...),
		fclk:      map[string]*widget.Clickable{},
		cards:     map[string]*widget.Clickable{},
		rows:      map[string]*widget.Clickable{},
		joinBtn:   map[string]*widget.Clickable{},
		caret:     map[string]*widget.Clickable{},
		member:    map[string]bool{},
		opSeq:     map[string]int{},
		wantFocus: 3,
	}
	d.rowMenu.sel = -1
	d.chanScope.labels = dirScopeLabels
	d.chanType.labels = dirTypeLabels
	d.acct.labels = dirAcctLabels
	for i := range d.srch {
		d.srch[i] = newSearch(dirPlaceholders[i])
		d.sorts[i].labels = dirSortLabels[i]
	}
	return d
}

// ShowTab selects a tab: 0 People, 1 Channels, 2 User Groups (3 External,
// 4 Invitations).
func (d *Directories) ShowTab(i int) {
	if i < 0 || i >= len(d.tab.Labels) {
		return
	}
	d.tab.Sel = i
	d.wantFocus = 3
	d.closeMenus()
}

func (d *Directories) closeMenus() {
	for i := range d.sorts {
		d.sorts[i].open = false
	}
	d.menuID, d.rowMenu.open, d.confirm = "", false, ""
	d.chanScope.open, d.chanType.open, d.acct.open = false, false, false
}

// Layout draws the view, filling the constraints.
func (d *Directories) Layout(gtx C) D {
	d.frame()
	size := gtx.Constraints.Max
	defer d.press.begin(gtx).Pop()
	fill(gtx, image.Rectangle{Max: size}, d.p.Surface)
	if d.tab.update(gtx) {
		d.closeMenus()
		d.wantFocus = 3
	}
	d.update(gtx)
	gtx.Constraints = layout.Exact(size)
	col(gtx,
		layout.Rigid(d.titleRow),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 16}.Layout(gtx, func(gtx C) D { return dirTabStrip(&d.base, gtx, d.tab, dirTabIcons, nil) })
		}),
		layout.Flexed(1, d.body),
	)
	return D{Size: size}
}

// titleRow: h 49, "Directories" 18px/24px 900 at x 18.
func (d *Directories) titleRow(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(49)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 18, Right: 18}.Layout(g, func(gtx C) D {
		return layout.W.Layout(gtx, func(gtx C) D { return d.label(gtx, 18, wBlack, d.p.Text, "Directories") })
	})
	return D{Size: image.Pt(w, h)}
}

// update runs searches (debounced), first loads, menus and pending writes.
func (d *Directories) update(gtx C) {
	t := d.tab.Sel
	d.pollOps()
	d.watchMembership()
	d.applyNotes()
	if t > dirTabGroups {
		return
	}
	if d.wantFocus > 0 {
		// The editor may not have been laid out yet; retry for a few frames.
		if gtx.Focused(&d.srch[t].ed) {
			d.wantFocus = 0
		} else {
			gtx.Execute(key.FocusCmd{Tag: &d.srch[t].ed})
			d.wantFocus--
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	if d.clear.Clicked(gtx) {
		d.srch[t].ed.SetText("")
		d.deb[t].set("", gtx.Now)
		d.deb[t].sent = ""
		d.load(t, "")
		gtx.Execute(key.FocusCmd{Tag: &d.srch[t].ed})
	}
	q, changed, submitted := d.srch[t].update(gtx)
	if changed {
		d.deb[t].set(q, gtx.Now)
	}
	if submitted {
		d.deb[t].set(q, gtx.Now)
		d.deb[t].sent = q
		d.load(t, q)
	} else if dq, ok := d.deb[t].due(gtx, dirDebounce); ok {
		d.load(t, dq)
	}
	switch t {
	case dirTabPeople:
		if !d.people.started() {
			d.load(t, q)
		}
	case dirTabChannels:
		if !d.chans.started() {
			d.load(t, q)
		}
	case dirTabGroups:
		if !d.groups.started() {
			d.load(t, q)
		}
	}
	if d.sorts[t].update(gtx) {
		d.load(t, q)
	}
	switch t {
	case dirTabPeople:
		if d.acct.update(gtx) {
			d.load(t, q)
		}
	case dirTabChannels:
		if d.chanScope.update(gtx) {
			d.chanType.open = false
			d.load(t, q)
		}
		if d.chanType.update(gtx) {
			d.chanScope.open = false
			d.load(t, q)
		}
	}
	if d.action[t].Clicked(gtx) || d.heroBtn[t].Clicked(gtx) {
		d.h.Toast(cdUnsupported)
	}
	if d.heroClose[t].Clicked(gtx) {
		d.heroOff[t] = true
	}
}

func (d *Directories) load(tab int, q string) {
	c := d.h.Core()
	switch tab {
	case dirTabPeople:
		d.peopleL.Position = layout.Position{}
		d.loadPeople(q, 1)
	case dirTabChannels:
		d.chanL.Position = layout.Position{}
		d.loadChannels(q, 1)
	case dirTabGroups:
		d.groupsQ = q
		d.groupL.Position = layout.Position{}
		gs := dirGroupSorts[min(d.sorts[dirTabGroups].sel, len(dirGroupSorts)-1)]
		excl := d.exclDisabled
		d.groups.load(d.h, true, func(ctx context.Context) ([]core.Usergroup, error) {
			out, err := c.SearchUsergroups(ctx, core.UsergroupOpts{Query: q, Sort: gs.sort, Desc: gs.desc, Count: 100, ExcludeDisabled: excl})
			if err != nil {
				// usergroups.search failed: fall back to the cached list.
				if fb, ferr := c.Usergroups(ctx, q, 100); ferr == nil {
					return fb, nil
				}
			}
			return out, err
		})
	}
}

// loadPeople pages the people directory (core.BrowsePeople: an empty
// query browses everyone, sorted and filtered server-side).
func (d *Directories) loadPeople(q string, page int) {
	d.peopleQ = q
	o := core.PeopleBrowseOpts{Query: q, Page: page, AccountType: dirAcctKeys[min(d.acct.sel, len(dirAcctKeys)-1)]}
	switch d.sorts[dirTabPeople].sel {
	case 1:
		o.Sort = "name"
	case 2:
		o.Sort, o.Desc = "name", true
	}
	var prev []core.User
	if page > 1 {
		cur, _, _ := d.people.get()
		prev = append([]core.User(nil), cur.list...)
	}
	c := d.h.Core()
	d.people.load(d.h, true, func(ctx context.Context) (dirPeople, error) {
		res, err := c.BrowsePeople(ctx, o)
		if err != nil {
			if page > 1 {
				return dirPeople{}, err
			}
			// Browse failed: fall back to the user search / the people
			// the client already knows, unsorted by the server.
			var us []core.User
			if q != "" {
				us, err = c.SearchUsers(ctx, q, 50)
			} else {
				us, err = dirKnownPeople(c), nil
			}
			return dirPeople{list: us, total: len(us), page: 1, pages: 1, q: q}, err
		}
		return dirPeople{list: append(prev, res.Users...), total: res.Total, page: res.Page, pages: res.Pages, q: q}, nil
	})
}

// dirKnownPeople lists the people in the core snapshot by name.
func dirKnownPeople(c *core.App) []core.User {
	snap := c.Snapshot()
	out := make([]core.User, 0, len(snap.Users))
	for _, u := range snap.Users {
		if u.IsBot || u.Deleted || u.ID == "USLACKBOT" {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(displayName(out[i])) < strings.ToLower(displayName(out[j]))
	})
	return out
}

// loadChannels pages the channel directory with the sort, scope and
// type filters (core.BrowseChannelsWith).
func (d *Directories) loadChannels(q string, page int) {
	cs := dirChanSorts[min(d.sorts[dirTabChannels].sel, len(dirChanSorts)-1)]
	o := core.ChannelBrowseOpts{Query: q, Page: page, Sort: cs.sort, Desc: cs.desc,
		Type:       dirTypeKeys[min(d.chanType.sel, len(dirTypeKeys)-1)],
		Membership: dirScopeKeys[min(d.chanScope.sel, len(dirScopeKeys)-1)]}
	var prev []core.ChannelHit
	if page > 1 {
		cur, _, _ := d.chans.get()
		prev = append([]core.ChannelHit(nil), cur.list...)
	}
	c := d.h.Core()
	d.chans.load(d.h, true, func(ctx context.Context) (dirChans, error) {
		res, err := c.BrowseChannelsWith(ctx, o)
		if err != nil {
			return dirChans{}, err
		}
		return dirChans{list: append(prev, res.Channels...), total: res.Total, page: res.Page, pages: res.Pages, q: q}, nil
	})
}

// startJoin joins or leaves optimistically; pollOps reverts on failure.
// Only called from a user's click. ch is the row as displayed.
func (d *Directories) startJoin(ch core.ChannelHit, join bool) {
	if d.pending(ch.ID) {
		return
	}
	d.opSeq[ch.ID]++
	d.member[ch.ID] = join
	o := &dirJoinOp{id: ch.ID, name: ch.Name, join: join, prev: ch.IsMember}
	c, id := d.h.Core(), ch.ID
	o.a.load(d.h, false, func(ctx context.Context) (bool, error) {
		if join {
			_, err := c.Join(ctx, id)
			return err == nil, err
		}
		return true, c.Leave(ctx, id)
	})
	d.ops = append(d.ops, o)
}

func (d *Directories) pollOps() {
	keep := d.ops[:0]
	for _, o := range d.ops {
		_, done, err := o.a.get()
		if !done {
			keep = append(keep, o)
			continue
		}
		if err != nil {
			if d.member[o.id] == o.join {
				d.member[o.id] = o.prev
			}
			verb := "join"
			if !o.join {
				verb = "leave"
			}
			d.h.Toast("Couldn’t " + verb + " #" + o.name + ": " + dirErr(err))
		}
	}
	for i := len(keep); i < len(d.ops); i++ {
		d.ops[i] = nil
	}
	d.ops = keep
}

// pending reports whether a Join/Leave for id is in flight.
func (d *Directories) pending(id string) bool {
	for _, o := range d.ops {
		if o.id == id {
			return true
		}
	}
	return false
}

// pendingOp returns the in-flight Join/Leave for id, if any.
func (d *Directories) pendingOp(id string) *dirJoinOp {
	for _, o := range d.ops {
		if o.id == id {
			return o
		}
	}
	return nil
}

// watchMembership subscribes to the conversation events that change
// membership: "conv" (joined, renamed, created; verified with a read since
// channel_created also emits it for channels the viewer isn't in) and
// "conv_removed" (left, archived, deleted).
func (d *Directories) watchMembership() {
	d.evq.start(d.h, func(ev core.Event) (dirMemberNote, bool) {
		switch ev.Type {
		case "conv":
			if ev.Conv != nil && isDMKind(ev.Conv.Kind) {
				return dirMemberNote{}, false
			}
			return dirMemberNote{id: ev.Channel, verify: true}, ev.Channel != ""
		case "conv_removed":
			return dirMemberNote{id: ev.Channel, member: false, ev: true}, ev.Channel != ""
		}
		return dirMemberNote{}, false
	})
}

// applyNotes folds queued realtime notes into the membership overrides,
// for channels in the loaded list only.
func (d *Directories) applyNotes() {
	notes := d.evq.drain()
	if len(notes) == 0 {
		return
	}
	st, _, _ := d.chans.get()
	listed := make(map[string]bool, len(st.list))
	for _, h := range st.list {
		listed[h.ID] = true
	}
	c, h := d.h.Core(), d.h
	for _, n := range notes {
		if !listed[n.id] {
			continue
		}
		switch {
		case n.verify:
			id, seq := n.id, d.opSeq[n.id]
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if inf, err := c.ChannelInfo(ctx, id); err == nil {
					d.evq.push(h, dirMemberNote{id: id, member: inf.IsMember, seq: seq})
				}
			}()
		case n.ev:
			if !d.pending(n.id) {
				d.member[n.id] = n.member
			}
		default: // a verification result
			if n.seq == d.opSeq[n.id] && !d.pending(n.id) {
				d.member[n.id] = n.member
			}
		}
	}
}

// eff is a hit as displayed: membership (and the member count) adjusted
// by the overrides.
func (d *Directories) eff(h core.ChannelHit) core.ChannelHit {
	if m, ok := d.member[h.ID]; ok && m != h.IsMember {
		h.IsMember = m
		if m {
			h.NumMembers++
		} else if h.NumMembers > 0 {
			h.NumMembers--
		}
	}
	return h
}

// ---- body ----

// body is the #f8f8f8 area under the tabs: the search row, then a list
// with the hero banner, the filter bar and the results.
func (d *Directories) body(gtx C) D {
	fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, d.p.ReadFill)
	t := d.tab.Sel
	switch t {
	case dirTabExternal:
		return d.emptyState(gtx, "globe", "External", "Connecting with people and channels outside Hack Club isn’t available in yoloslack yet.", nil, "")
	case dirTabInvites:
		return d.emptyState(gtx, "send", "Invitations", "Managing invitations isn’t available in yoloslack yet.", nil, "")
	}
	return col(gtx,
		vgap(16),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D {
						return dirSearchField(&d.base, gtx, d.srch[t], dirFieldStyle{H: 36, Icon: 20, PadL: 12, Gap: 16, Border: dirRing(&d.base, d.p.Surface), Over: d.p.ReadFill}, &d.clear)
					}),
					hgap(12),
					layout.Rigid(func(gtx C) D { return d.button(gtx, &d.action[t], btnOutline, false, dirActions[t]) }),
				)
			})
		}),
		vgap(16),
		layout.Flexed(1, func(gtx C) D {
			switch t {
			case dirTabPeople:
				return d.peopleTab(gtx)
			case dirTabChannels:
				return d.chansTab(gtx)
			}
			return d.groupsTab(gtx)
		}),
	)
}

// heads returns the list items above the results: hero, filter bar.
func (d *Directories) heads(t int) []W {
	var out []W
	if dirHeroTitle[t] != "" && !d.heroOff[t] {
		out = append(out, func(gtx C) D { return d.hero(gtx, t) })
	}
	return append(out, func(gtx C) D { return d.filterBar(gtx, t) })
}

// hero is the dismissible #e3f8ff banner: full width with a 4px inset,
// h 246 incl. 12px vertical padding, content at x 79, close button at the
// top right; 30px gap below.
func (d *Directories) hero(gtx C, t int) D {
	w := gtx.Constraints.Max.X
	h := gtx.Dp(246)
	in := gtx.Dp(4)
	rrect(gtx, image.Rect(in, 0, w-in, h), gtx.Dp(8), d.p.PillOnBg)
	x := in + gtx.Dp(75)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(min(gtx.Dp(517), max(0, w-x-gtx.Dp(64))), h)}
	cd := col(g,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 22, 30, wBlack, d.p.Text, 2, dirHeroTitle[t]) })
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Muted, 0, dirHeroBody[t]) })
		}),
		layout.Rigid(func(gtx C) D { return d.button(gtx, &d.heroBtn[t], btnOutline, false, dirHeroBtn[t]) }),
	)
	call := m.Stop()
	func() {
		defer op.Offset(image.Pt(x, max(gtx.Dp(12), (h-cd.Size.Y)/2))).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	func() {
		defer op.Offset(image.Pt(w-gtx.Dp(16+36), gtx.Dp(17))).Push(gtx.Ops).Pop()
		d.iconButton(gtx, &d.heroClose[t], "close", 36, 20, d.p.Sec)
	}()
	return D{Size: image.Pt(w, h+gtx.Dp(30))}
}

// filterBar: h 28 filter buttons 8px apart, the sort select right-aligned,
// 16px above the results.
func (d *Directories) filterBar(gtx C, t int) D {
	return layout.Inset{Left: dirPad, Right: dirPad, Bottom: 16}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ch := make([]layout.FlexChild, 0, 2*len(dirFilters[t])+2)
		for i, f := range dirFilters[t] {
			if dd := d.filterMenu(t, i); dd != nil {
				ch = append(ch,
					layout.Rigid(func(gtx C) D {
						label := dd.labels[min(dd.sel, len(dd.labels)-1)]
						if dd == &d.acct && dd.sel == 0 {
							label = f.label
						}
						dims := dirFilterBtn(&d.base, gtx, &dd.btn, f.icon, label, f.caret)
						if dd.open {
							dd.menu(&d.base, gtx, dims.Size, false)
						}
						return dims
					}),
					hgap(8))
				continue
			}
			c := dirClick(d.fclk, dirTabLabels[t]+"/"+f.label)
			toggle := t == dirTabGroups && f.label == "Exclude deactivated"
			if c.Clicked(gtx) {
				if toggle {
					d.exclDisabled = !d.exclDisabled
					d.load(dirTabGroups, d.groupsQ)
				} else {
					d.h.Toast("Filtering by " + strings.ToLower(f.label) + " isn’t supported yet")
				}
			}
			icon := f.icon
			if toggle && d.exclDisabled {
				icon = "check"
			}
			ch = append(ch,
				layout.Rigid(func(gtx C) D { return dirFilterBtn(&d.base, gtx, c, icon, f.label, f.caret) }),
				hgap(8))
		}
		ch = append(ch,
			layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
			layout.Rigid(func(gtx C) D {
				dd := &d.sorts[t]
				label := dd.labels[dd.sel]
				if t == dirTabChannels && dd.sel == 0 && d.srch[t].text() != "" {
					label = "Most relevant"
				}
				return dd.layoutSort(&d.base, gtx, label)
			}))
		return row(gtx, layout.Middle, ch...)
	})
}

// filterMenu returns the dropdown behind filter button i of tab t, or nil
// for filters core can't apply yet.
func (d *Directories) filterMenu(t, i int) *dirDropdown {
	switch {
	case t == dirTabPeople && i == 4:
		return &d.acct
	case t == dirTabChannels && i == 0:
		return &d.chanScope
	case t == dirTabChannels && i == 1:
		return &d.chanType
	}
	return nil
}

// Filter menus and their core option values.
var (
	dirScopeLabels = []string{"All channels", "My channels", "Other channels"}
	dirScopeKeys   = []string{"", "joined", "other"}
	dirTypeLabels  = []string{"Any channel type", "Public channels", "Private channels", "Archived channels", "External channels"}
	dirTypeKeys    = []string{"", "public", "private", "archived", "external"}
	dirAcctLabels  = []string{"Any account type", "Owners", "Admins", "Full members", "Guests", "Deactivated accounts"}
	dirAcctKeys    = []string{"", "owners", "admins", "members", "guests", "deactivated"}
)

// dirFilterBtn: h 28, radius 8, white, 1px hairline, 13px bold label with
// an optional leading icon and trailing 16px caret.
func dirFilterBtn(b *base, gtx C, c *widget.Clickable, icon, label string, caret bool) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		h := gtx.Dp(28)
		padL, padR := gtx.Dp(12), gtx.Dp(12)
		if icon != "" {
			padL = gtx.Dp(8)
		}
		if caret {
			padR = gtx.Dp(4)
		}
		tw := b.th.Measure(gtx, 13, wBold, label)
		w := padL + tw + padR
		if icon != "" {
			w += gtx.Dp(16 + 4)
		}
		if caret {
			w += gtx.Dp(16)
		}
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, b.p.Surface)
		if c.Hovered() {
			rrect(gtx, r, rad, b.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Border)
		x := padL
		if icon != "" {
			dirAt(gtx, image.Pt(x, (h-gtx.Dp(16))/2), func(gtx C) D { return b.icon(gtx, icon, 16, b.p.Sec) })
			x += gtx.Dp(16 + 4)
		}
		dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(tw, h))
			return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wBold, b.p.Text, label) })
		})
		if caret {
			dirAt(gtx, image.Pt(x+tw, (h-gtx.Dp(16))/2), func(gtx C) D { return b.icon(gtx, "caret-down", 16, b.p.Sec) })
		}
		return D{Size: r.Max}
	})
}

// ---- people ----

// dirGrid sizes the people grid for width w: columns, card width, gap.
func dirGrid(gtx C, w int) (cols, cw, gap int) {
	gap = gtx.Dp(dirCardGap)
	cols = max(1, (w+gap)/(gtx.Dp(dirCardMin)+gap))
	cw = (w - gap*(cols-1)) / cols
	return cols, max(cw, 1), gap
}

func (d *Directories) peopleTab(gtx C) D {
	st, loaded, err := d.people.get()
	us := st.list
	head := d.heads(dirTabPeople)
	inner := gtx.Constraints.Max.X - 2*gtx.Dp(dirPad)
	cols, cw, gap := dirGrid(gtx, inner)
	cardH := cw + gtx.Dp(dirCardText)
	rows := (len(us) + cols - 1) / cols
	body := max(rows, 1)
	busy := d.people.busy()
	if p := d.peopleL.Position; loaded && err == nil && !busy && st.page < st.pages && p.First+p.Count >= len(head)+rows-2 {
		d.loadPeople(st.q, st.page+1)
		busy = true
	}
	return d.scrollList(gtx, &d.peopleL, len(head)+body+1, func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if i < len(head) {
			return head[i](gtx)
		}
		i -= len(head)
		switch {
		case i == body:
			if busy && len(us) > 0 {
				return layout.Inset{Top: 4, Bottom: 24}.Layout(gtx, func(gtx C) D {
					return d.centered(gtx, 13, wReg, d.p.Muted, "Loading more people…")
				})
			}
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(24))}
		case !loaded:
			return d.gridSkeleton(gtx, cols, cw, gap, cardH)
		case err != nil && len(us) == 0:
			if d.retry.Clicked(gtx) {
				d.load(dirTabPeople, d.peopleQ)
			}
			return dirListState(gtx, func(gtx C) D { return d.errorState(gtx, err, &d.retry) })
		case len(us) == 0:
			if d.peopleQ == "" {
				return dirListState(gtx, func(gtx C) D {
					return d.emptyState(gtx, "people", "Find people", "Search by name or title to find anyone in Hack Club.", nil, "")
				})
			}
			return dirListState(gtx, func(gtx C) D {
				return d.emptyState(gtx, "search", "No results", "No one matches “"+d.peopleQ+"”. Check the spelling or try another name.", nil, "")
			})
		}
		return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, func(gtx C) D {
			for c := 0; c < cols; c++ {
				idx := i*cols + c
				if idx >= len(us) {
					break
				}
				dirAt(gtx, image.Pt(c*(cw+gap), 0), func(gtx C) D {
					gtx.Constraints = layout.Exact(image.Pt(cw, cardH))
					return d.personCard(gtx, us[idx], cw, cardH)
				})
			}
			return D{Size: image.Pt(gtx.Constraints.Max.X, cardH+gap)}
		})
	})
}

// personCard: 181x281 white card, radius 8, 1px #1d1c1d21 ring; square
// photo (radius 8 8 0 0); info padded 10px 16px with the name (15/22 700,
// 2 lines) followed by presence and status emoji, then "title · pronouns"
// (13/18, 3 lines). Hover adds a soft shadow and a #1d1c1d@.04 overlay.
func (d *Directories) personCard(gtx C, u core.User, w, h int) D {
	c := dirClick(d.cards, u.ID)
	if c.Clicked(gtx) {
		d.h.OpenProfile(d.press.at, u.ID)
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		rad := gtx.Dp(8)
		card := image.Rect(0, 0, w, h)
		if c.Hovered() {
			rrect(gtx, card.Add(image.Pt(0, gtx.Dp(1))).Inset(-gtx.Dp(1)), rad+gtx.Dp(1), flatten(argb(0x14000000), d.p.ReadFill))
		}
		// The hover overlay is flattened over the card surface (Gio blends
		// in linear space, which would make a translucent tint too strong)
		// and stays translucent only over the photo.
		bg := d.p.Surface
		if c.Hovered() {
			bg = flatten(dirHoverTint(&d.base), d.p.Surface)
		}
		rrect(gtx, card, rad, bg)
		func() {
			defer clip.RRect{Rect: image.Rect(0, 0, w, w), NW: rad, NE: rad}.Push(gtx.Ops).Pop()
			fill(gtx, image.Rect(0, 0, w, w), d.p.Track)
			if url := dirFirst(u.Avatar512, u.Avatar192, u.Avatar); url != "" && d.h.Images() != nil {
				if img, ok := d.h.Images().Get(url, w); ok {
					theme.Image(gtx, img, image.Pt(w, w), 0)
				}
			}
			if c.Hovered() {
				fill(gtx, image.Rect(0, 0, w, w), theme.LinearAlpha(dirHoverTint(&d.base), d.p.Surface))
			}
		}()
		dirAt(gtx, image.Pt(gtx.Dp(16), w+gtx.Dp(10)), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-gtx.Dp(32), h-w-gtx.Dp(20))}
			return col(gtx,
				layout.Rigid(func(gtx C) D { return dirNameMarks(&d.base, gtx, u, displayName(u), 15, wBold, 2) }),
				layout.Rigid(func(gtx C) D {
					sub := dirJoin(" · ", u.Title, u.Pronouns)
					if sub == "" {
						return D{}
					}
					return d.para(gtx, 13, 18, wReg, d.p.Muted, 3, sub)
				}),
			)
		})
		border(gtx, card, rad, max(1, gtx.Dp(1)), dirRing(&d.base, d.p.ReadFill))
		return D{Size: card.Max}
	})
}

// dirNameMarks draws a name (wrapping to maxLines) followed by the 20px
// presence icon and a 16px status emoji, keeping room for them.
func dirNameMarks(b *base, gtx C, u core.User, name string, size unit.Sp, w font.Weight, maxLines int) D {
	extra := 0
	if u.Presence != "" {
		extra += gtx.Dp(20)
	}
	if u.StatusEmojiURL != "" {
		extra += gtx.Dp(20)
	}
	return row(gtx, layout.Start,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-extra)
			gtx.Constraints.Min.X = 0
			return b.para(gtx, size, unit.Sp(float32(size)*22/15), w, b.p.Text, maxLines, name)
		}),
		layout.Rigid(func(gtx C) D {
			if u.Presence == "" {
				return D{}
			}
			return layout.Inset{Top: unit.Dp(float32(size)*22/15-20) / 2}.Layout(gtx, func(gtx C) D { return dirPresence(b, gtx, u.Presence) })
		}),
		layout.Rigid(func(gtx C) D {
			if u.StatusEmojiURL == "" {
				return D{}
			}
			return layout.Inset{Left: 4, Top: unit.Dp(float32(size)*22/15-16) / 2}.Layout(gtx, func(gtx C) D {
				return b.emojiImg(gtx, u.StatusEmojiURL, 16)
			})
		}),
	)
}

// dirJoin joins the non-empty parts with sep.
func dirJoin(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func (d *Directories) gridSkeleton(gtx C, cols, cw, gap, cardH int) D {
	rad := gtx.Dp(8)
	return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, func(gtx C) D {
		for r := 0; r < 2; r++ {
			for i := 0; i < cols; i++ {
				dirAt(gtx, image.Pt(i*(cw+gap), r*(cardH+gap)), func(gtx C) D {
					rrect(gtx, image.Rect(0, 0, cw, cardH), rad, d.p.Surface)
					func() {
						defer clip.RRect{Rect: image.Rect(0, 0, cw, cw), NW: rad, NE: rad}.Push(gtx.Ops).Pop()
						fill(gtx, image.Rect(0, 0, cw, cw), d.p.Track)
					}()
					y := cw + gtx.Dp(14)
					x := gtx.Dp(16)
					rrect(gtx, image.Rect(x, y, x+(cw-2*x)*(55+(i*13)%30)/100, y+gtx.Dp(12)), gtx.Dp(4), d.p.Skel)
					y += gtx.Dp(22)
					rrect(gtx, image.Rect(x, y, x+(cw-2*x)*(35+(i*17)%30)/100, y+gtx.Dp(10)), gtx.Dp(4), d.p.Skel)
					border(gtx, image.Rect(0, 0, cw, cardH), rad, max(1, gtx.Dp(1)), dirRing(&d.base, d.p.ReadFill))
					return D{}
				})
			}
		}
		return D{Size: image.Pt(gtx.Constraints.Max.X, 2*(cardH+gap))}
	})
}

// dirListState lays out an empty/error state inside a list (finite height).
func dirListState(gtx C, w W) D {
	h := gtx.Dp(280)
	gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, h))
	w(gtx)
	return D{Size: gtx.Constraints.Max}
}

// ---- channels ----

func (d *Directories) chansTab(gtx C) D {
	st, loaded, err := d.chans.get()
	head := d.heads(dirTabChannels)
	list := st.list
	n := len(list)
	busy := d.chans.busy()
	filtering := d.chanType.sel != 0 || d.chanScope.sel != 0
	if p := d.chanL.Position; loaded && err == nil && !busy && st.page < st.pages && p.First+p.Count >= len(head)+n-5 {
		d.loadChannels(st.q, st.page+1)
		busy = true
	}
	body := max(n, 1)
	return d.scrollList(gtx, &d.chanL, len(head)+body+1, func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if i < len(head) {
			return head[i](gtx)
		}
		i -= len(head)
		switch {
		case i == body:
			if busy && n > 0 {
				return layout.Inset{Top: 12, Bottom: 24}.Layout(gtx, func(gtx C) D {
					return d.centered(gtx, 13, wReg, d.p.Muted, "Loading more channels…")
				})
			}
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(24))}
		case !loaded:
			return layout.Inset{Left: dirPad - 20, Right: dirPad - 20}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 6, 36) })
		case err != nil && n == 0:
			if d.retry.Clicked(gtx) {
				d.load(dirTabChannels, d.srch[dirTabChannels].text())
			}
			return dirListState(gtx, func(gtx C) D { return d.errorState(gtx, err, &d.retry) })
		case n == 0:
			if filtering {
				return dirListState(gtx, func(gtx C) D {
					return d.emptyState(gtx, "hash", "No matching channels", "No channels match these filters. Try another channel type or scope.", nil, "")
				})
			}
			if st.q == "" {
				return dirListState(gtx, func(gtx C) D {
					return d.emptyState(gtx, "hash", "No channels", "There aren’t any channels to browse yet.", nil, "")
				})
			}
			return dirListState(gtx, func(gtx C) D {
				return d.emptyState(gtx, "search", "No results", "No channels match “"+st.q+"”. Try a different name.", nil, "")
			})
		}
		return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, func(gtx C) D {
			return d.chanRow(gtx, list[i], i == 0, i == n-1)
		})
	})
}

// chanRow: white row in a bordered list (radius 12 on the ends), padding
// 16, h 77. "# name" 15/22 700 ("View channel" on hover), then
// "✓ Joined · N members · purpose" 13/18 #454447. On hover a Join/Leave
// split button. The row's clickable is a sibling below its buttons so a
// button click doesn't also open the channel.
func (d *Directories) chanRow(gtx C, ch core.ChannelHit, first, last bool) D {
	ch = d.eff(ch)
	rc := dirClick(d.rows, ch.ID)
	jb := dirClick(d.joinBtn, ch.ID)
	cb := dirClick(d.caret, ch.ID)
	confirming := d.confirm == ch.ID
	if jb.Clicked(gtx) && !ch.IsArchived {
		switch {
		case ch.IsMember && ch.Kind == "private":
			// Leaving a private channel can't be undone without an invite.
			d.confirm, d.menuID = ch.ID, ""
		default:
			d.startJoin(ch, !ch.IsMember)
		}
	}
	if confirming {
		if d.confNo.Clicked(gtx) {
			d.confirm, confirming = "", false
		}
		if d.confYes.Clicked(gtx) {
			d.confirm, confirming = "", false
			d.startJoin(ch, false)
		}
	}
	if cb.Clicked(gtx) {
		if d.menuID == ch.ID && d.rowMenu.open {
			d.menuID, d.rowMenu.open = "", false
		} else {
			d.menuID, d.rowMenu.open, d.confirm, confirming = ch.ID, true, "", false
		}
	}
	menuOpen := d.menuID == ch.ID && d.rowMenu.open
	if menuOpen {
		d.rowMenuUpdate(gtx, ch)
		menuOpen = d.menuID == ch.ID && d.rowMenu.open
	}
	if rc.Clicked(gtx) {
		d.h.Navigate("c/" + ch.ID)
	}
	pend := d.pendingOp(ch.ID)
	hover := rc.Hovered() || jb.Hovered() || cb.Hovered()
	req := ch.IsMember && d.required(ch.ID)
	showBtn := (hover || menuOpen || pend != nil) && !ch.IsArchived && !req
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 16, Bottom: 16, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D {
				return dirFill(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return d.chanName(gtx, ch, hover, req) }),
						vgap(4),
						layout.Rigid(func(gtx C) D {
							if confirming {
								return d.para(gtx, 13, 18, wReg, d.p.Sec, 1, "Leave #"+ch.Name+"? You won’t be able to rejoin unless someone invites you.")
							}
							return d.chanMeta(gtx, ch)
						}),
					)
				})
			}),
			layout.Rigid(func(gtx C) D {
				if confirming {
					return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D { return d.button(gtx, &d.confNo, btnOutline, false, "Cancel") }),
							hgap(8),
							layout.Rigid(func(gtx C) D { return d.button(gtx, &d.confYes, btnDanger, false, "Leave") }),
						)
					})
				}
				if !showBtn {
					return D{}
				}
				label := "Join"
				if ch.IsMember {
					label = "Leave"
				}
				if pend != nil {
					label = "Joining…"
					if !pend.join {
						label = "Leaving…"
					}
				}
				var menu func(gtx C, sz image.Point)
				if menuOpen {
					menu = func(gtx C, sz image.Point) { d.rowMenu.menu(&d.base, gtx, sz, true) }
				}
				return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D { return dirSplitBtn(&d.base, gtx, jb, cb, label, menu) })
			}),
		)
	})
	call := m.Stop()

	dirRowShape(&d.base, gtx, dims.Size, first, last)
	g := gtx
	g.Constraints = layout.Exact(dims.Size)
	rc.Layout(g, func(gtx C) D {
		pointerCursor(gtx)
		return D{Size: dims.Size}
	})
	call.Add(gtx.Ops)
	return dims
}

// dirRowMenu is the Join/Leave caret menu.
var dirRowMenu = []string{"View channel", "Copy link"}

// rowMenuUpdate handles the open caret menu of row ch.
func (d *Directories) rowMenuUpdate(gtx C, ch core.ChannelHit) {
	dd := &d.rowMenu
	dd.labels = dirRowMenu
	dd.sel = -1
	if !dd.update(gtx) {
		if !dd.open {
			d.menuID = ""
		}
		return
	}
	pick := dd.sel
	dd.sel, dd.open, d.menuID = -1, false, ""
	switch pick {
	case 0:
		d.h.Navigate("c/" + ch.ID)
	case 1:
		cdCopy(gtx, cdLinkPrefix+ch.ID)
		d.h.Toast("Link copied to clipboard")
	}
}

func (d *Directories) chanName(gtx C, ch core.ChannelHit, hover, req bool) D {
	ic := "hash"
	if ch.Kind == "private" {
		ic = "lock"
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D { return d.icon(gtx, ic, 15, d.p.Text) }),
		hgap(2),
		layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, ch.Name) }),
		layout.Rigid(func(gtx C) D {
			if !hover {
				return D{}
			}
			return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, "View channel") })
		}),
		layout.Rigid(func(gtx C) D {
			if !req {
				return D{}
			}
			return layout.Inset{Left: 24}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, "Required") })
		}),
	)
}

func (d *Directories) chanMeta(gtx C, ch core.ChannelHit) D {
	sep := func() layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 6, Right: 6}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Sec, "·") })
		})
	}
	var parts []layout.FlexChild
	add := func(ch ...layout.FlexChild) {
		if len(parts) > 0 {
			parts = append(parts, sep())
		}
		parts = append(parts, ch...)
	}
	switch {
	case ch.IsArchived:
		add(layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Sec, "Archived") }))
	case ch.IsMember:
		add(layout.Rigid(func(gtx C) D { return d.icon(gtx, "check", 13, d.joinedColor()) }),
			hgap(2),
			layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wBold, d.joinedColor(), "Joined") }))
	}
	// The client snapshot (My channels) has no member counts; a channel
	// never really has 0 members, so 0 means unknown.
	if ch.NumMembers > 0 {
		add(layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Sec, dirMembers(ch.NumMembers)) }))
	}
	if p := d.plainPurpose(ch.Purpose); p != "" {
		add(layout.Flexed(1, func(gtx C) D {
			return dirFill(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Sec, p) })
		}))
	}
	return row(gtx, layout.Middle, parts...)
}

// joinedColor is the "✓ Joined" green (#007a5a; the brighter presence
// green in dark mode, where #007a5a is unreadable on the dark surface).
func (d *Directories) joinedColor() color.NRGBA {
	if d.th.Dark {
		return d.p.Presence
	}
	return d.p.Green
}

// plainPurpose renders a purpose's mrkdwn (<#C…>, <@U…>, <url|label>) to
// one plain line, cached by the raw text.
func (d *Directories) plainPurpose(raw string) string {
	if !strings.ContainsAny(raw, "<&*_~`") {
		return oneLine(raw)
	}
	if v, ok := d.purp[raw]; ok {
		return v
	}
	if d.purp == nil || len(d.purp) > 2000 {
		d.purp = map[string]string{}
	}
	v := oneLine(richtext.PlainText(richtext.Doc{Blocks: d.h.Core().Mrkdwn(raw)}))
	if v == "" {
		v = oneLine(raw)
	}
	d.purp[raw] = v
	return v
}

// required reports whether a channel can't be left (the default
// #announcements-style channel); Slack shows "Required" and no button.
func (d *Directories) required(id string) bool {
	c, ok := d.h.Core().Conv(id)
	return ok && c.IsGeneral
}

// dirErr is a short reason for a failed Join/Leave.
func dirErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "cant_leave_general"):
		return "it’s the default channel, so everyone stays in it"
	case strings.Contains(s, "is_archived"):
		return "the channel is archived"
	case strings.Contains(s, "restricted_action"), strings.Contains(s, "not_allowed"):
		return "an admin restricted this"
	case strings.Contains(s, "channel_not_found"), strings.Contains(s, "method_not_supported_for_channel_type"):
		return "the channel isn’t available"
	}
	return friendlyErr(err)
}

// dirSplitBtn is the outline medium split button: an 80x36 label part
// (radius 8 0 0 8) and a 41x36 caret part (radius 0 8 8 0).
// menu, when set, is drawn in the caret part's frame (the caret menu).
func dirSplitBtn(b *base, gtx C, main, caret *widget.Clickable, label string, menu func(gtx C, sz image.Point)) D {
	h, mw, cw := gtx.Dp(36), gtx.Dp(80), gtx.Dp(41)
	rad := gtx.Dp(8)
	bw := float32(max(1, gtx.Dp(1)))
	part := func(c *widget.Clickable, x, w int, rr clip.RRect, inner W) {
		dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(w, h))
			return c.Layout(gtx, func(gtx C) D {
				pointerCursor(gtx)
				bg := b.p.Surface
				if c.Hovered() {
					bg = b.p.ReadFill
				}
				paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
				paint.FillShape(gtx.Ops, b.p.BorderStr, clip.Stroke{Path: rr.Path(gtx.Ops), Width: bw}.Op())
				return layout.Center.Layout(gtx, inner)
			})
		})
	}
	part(main, 0, mw, clip.RRect{Rect: image.Rect(0, 0, mw, h), NW: rad, SW: rad}, func(gtx C) D {
		return b.label(gtx, 15, wBold, b.p.Text, label)
	})
	part(caret, mw, cw, clip.RRect{Rect: image.Rect(0, 0, cw, h), NE: rad, SE: rad}, func(gtx C) D {
		return b.icon(gtx, "caret-down", 16, b.p.Sec)
	})
	if menu != nil {
		dirAt(gtx, image.Pt(mw, 0), func(gtx C) D {
			menu(gtx, image.Pt(cw, h))
			return D{}
		})
	}
	return D{Size: image.Pt(mw+cw, h)}
}

// dirMembers formats "1 member" / "1,234 members".
func dirMembers(n int) string {
	if n == 1 {
		return "1 member"
	}
	return humanCount(n) + " members"
}

// ---- user groups ----

func (d *Directories) groupsTab(gtx C) D {
	gs, loaded, err := d.groups.get()
	head := d.heads(dirTabGroups)
	n := len(gs)
	body := max(n, 1)
	return d.scrollList(gtx, &d.groupL, len(head)+body+1, func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		if i < len(head) {
			return head[i](gtx)
		}
		i -= len(head)
		switch {
		case i == body:
			return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(24))}
		case !loaded:
			return layout.Inset{Left: dirPad - 20, Right: dirPad - 20}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 6, 36) })
		case err != nil && n == 0:
			if d.retry.Clicked(gtx) {
				d.load(dirTabGroups, d.groupsQ)
			}
			return dirListState(gtx, func(gtx C) D { return d.errorState(gtx, err, &d.retry) })
		case n == 0:
			if d.groupsQ == "" {
				return dirListState(gtx, func(gtx C) D {
					return d.emptyState(gtx, "people", "No user groups", "Hack Club doesn’t have any user groups yet.", nil, "")
				})
			}
			return dirListState(gtx, func(gtx C) D {
				return d.emptyState(gtx, "search", "No results", "No user groups match “"+d.groupsQ+"”.", nil, "")
			})
		}
		return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, func(gtx C) D { return d.groupRow(gtx, gs[i], i == 0, i == n-1) })
	})
}

// groupRow: h 65, padding 12px 20px. Name 15 700 then "@handle" 15 400
// (8px apart); "N members · description" 13/18 muted.
func (d *Directories) groupRow(gtx C, g core.Usergroup, first, last bool) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 12, Bottom: 12, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		return dirFill(gtx, func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return row(gtx, layout.Baseline,
						layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, dirFirst(g.Name, g.Handle)) }),
						layout.Flexed(1, func(gtx C) D {
							return dirFill(gtx, func(gtx C) D {
								return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.label(gtx, 15, wReg, d.p.Text, "@"+g.Handle) })
							})
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					return d.label(gtx, 13, wReg, d.p.Muted, dirJoin(" · ", dirMembers(g.UserCount), oneLine(g.Description)))
				}),
			)
		})
	})
	call := m.Stop()
	dirRowShape(&d.base, gtx, dims.Size, first, last)
	call.Add(gtx.Ops)
	return dims
}

// ---- shared helpers (also used by details.go) ----

// dirClick returns the persistent clickable for key k.
func dirClick(m map[string]*widget.Clickable, k string) *widget.Clickable {
	c, ok := m[k]
	if !ok {
		c = new(widget.Clickable)
		m[k] = c
	}
	return c
}

func dirFirst(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// dirAt lays out w at offset pt with loose constraints.
func dirAt(gtx C, pt image.Point, w W) D {
	defer op.Offset(pt).Push(gtx.Ops).Pop()
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max.X = max(0, gtx.Constraints.Max.X-pt.X)
	return w(gtx)
}

// dirFill lays out w and reports the full available width, so a Flexed
// child that draws something narrower keeps its neighbours in place.
func dirFill(gtx C, w W) D {
	g := gtx
	g.Constraints.Min.X = 0
	d := w(g)
	d.Size.X = gtx.Constraints.Max.X
	return d
}

// dirImage draws an image (photo) at size with radius, or a placeholder.
func dirImage(b *base, gtx C, url string, size image.Point, rad int) D {
	if url != "" && b.h.Images() != nil {
		if img, ok := b.h.Images().Get(url, max(size.X, size.Y)); ok {
			return theme.Image(gtx, img, size, rad)
		}
	}
	rrect(gtx, image.Rectangle{Max: size}, rad, b.p.Track)
	return D{Size: size}
}

// dirPresence is Slack's 20px presence icon: a filled #20a271 dot when
// active, a hollow #5e5d60 ring when away.
func dirPresence(b *base, gtx C, presence string) D {
	s := gtx.Dp(20)
	dot := gtx.Dp(10)
	c := image.Pt(s/2, s/2)
	if presence == "active" {
		theme.Circle(gtx, c, dot, dirActive)
	} else {
		theme.Circle(gtx, c, dot, b.p.Muted)
		theme.Circle(gtx, c, dot-2*max(1, gtx.Dp(1.5)), b.p.Surface)
	}
	return D{Size: image.Pt(s, s)}
}

// dirRaw picks a raw (unflattened) palette token for the theme, for
// translucent literals composited over a known background with flatten.
func dirRaw(b *base, f func(p *pal) color.NRGBA) color.NRGBA {
	if b.th != nil && b.th.Dark {
		return f(&darkPal)
	}
	return f(&lightPal)
}

// dirListBorder is the bordered-list hairline (#1d1c1d@0.1) over the gray
// body.
func dirListBorder(b *base) color.NRGBA {
	return flatten(dirRaw(b, func(p *pal) color.NRGBA { return p.CardLine }), b.p.ReadFill)
}

// dirRing is the 1px #1d1c1d@.13 ring of cards and field groups, flattened
// over the background it is drawn on.
func dirRing(b *base, over color.NRGBA) color.NRGBA {
	return flatten(dirRaw(b, func(p *pal) color.NRGBA { return p.Ring }), over)
}

// dirHoverTint is the card hover overlay (#1d1c1d@.04; white@.04 in dark).
// It stays translucent: it covers the photo too.
func dirHoverTint(b *base) color.NRGBA {
	if b.th != nil && b.th.Dark {
		return argb(0x0ae8e8e8)
	}
	return argb(0x0a1d1c1d)
}

// dirRowShape paints one row of a bordered list: white fill, 1px border,
// radius 12 at the top of the first row and the bottom of the last, a
// 0 1px 1px rgba(0,0,0,.06) shadow under the last. Non-last rows extend
// 1px down so the next row's top border is the only divider.
func dirRowShape(b *base, gtx C, sz image.Point, first, last bool) {
	r := gtx.Dp(12)
	rect := image.Rectangle{Max: sz}
	if !last {
		rect.Max.Y += max(1, gtx.Dp(1))
	}
	rr := clip.RRect{Rect: rect}
	if first {
		rr.NW, rr.NE = r, r
	}
	if last {
		rr.SW, rr.SE = r, r
		sh := rr
		sh.Rect = rect.Add(image.Pt(0, max(1, gtx.Dp(1))))
		paint.FillShape(gtx.Ops, flatten(argb(0x0f000000), b.p.ReadFill), sh.Op(gtx.Ops))
	}
	paint.FillShape(gtx.Ops, b.p.Surface, rr.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, dirListBorder(b), clip.Stroke{Path: rr.Path(gtx.Ops), Width: float32(max(1, gtx.Dp(1)))}.Op())
}

// dirTabStrip draws Slack's c-tabs: text-width tabs, padding 8px 0 (h 32),
// margin 0 20px 0 8px, 13px/16px bold; selected #1d1c1d with a 2px accent
// underline, others #454447; optional 16px icons (gap 4) and counts
// (13px 400 #454447, margin-left 4).
func dirTabStrip(b *base, gtx C, t *tabs, icons, counts []string) D {
	ch := make([]layout.FlexChild, 0, len(t.Labels))
	for i := range t.Labels {
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 8, Right: 20}.Layout(gtx, func(gtx C) D {
				return t.clicks[i].Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					sel := i == t.Sel
					fg := b.p.Sec
					if sel || t.clicks[i].Hovered() {
						fg = b.p.Text
					}
					d := layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
						return row(gtx, layout.Middle,
							layout.Rigid(func(gtx C) D {
								if i >= len(icons) || icons[i] == "" {
									return D{}
								}
								return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icons[i], 16, fg) })
							}),
							layout.Rigid(func(gtx C) D { return b.label(gtx, 13, wBold, fg, t.Labels[i]) }),
							layout.Rigid(func(gtx C) D {
								if i >= len(counts) || counts[i] == "" {
									return D{}
								}
								return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wReg, b.p.Sec, counts[i]) })
							}),
						)
					})
					if sel {
						h := gtx.Dp(2)
						fill(gtx, image.Rect(0, d.Size.Y-h, d.Size.X, d.Size.Y), b.p.TabLine)
					}
					return d
				})
			})
		}))
	}
	return row(gtx, layout.End, ch...)
}

// dirFieldStyle configures dirSearchField.
type dirFieldStyle struct {
	H, Icon, PadL, Gap unit.Dp
	Border             color.NRGBA
	Over               color.NRGBA // background under the field (focus ring is flattened over it)
}

// dirSearchField is a search input: radius 8, white, 1px border, a
// magnifier, 15px text; focused it gets Slack's ring (0 0 0 1px #1264a3,
// 0 0 0 5px #1264a3@.3). clear, when set, shows "Clear" while non-empty.
func dirSearchField(b *base, gtx C, s *search, st dirFieldStyle, clr *widget.Clickable) D {
	hgt := gtx.Dp(st.H)
	w := gtx.Constraints.Max.X
	r := image.Rect(0, 0, w, hgt)
	rad := gtx.Dp(8)
	if gtx.Focused(&s.ed) {
		ring := gtx.Dp(5)
		rrect(gtx, r.Inset(-ring), rad+ring, flatten(theme.WithAlpha(b.p.Link, 0x4d), st.Over))
		one := max(1, gtx.Dp(1))
		rrect(gtx, r.Inset(-one), rad+one, b.p.Link)
	}
	rrect(gtx, r, rad, b.p.Surface)
	border(gtx, r, rad, max(1, gtx.Dp(1)), st.Border)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, hgt))
	layout.Inset{Left: st.PadL, Right: 12}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return b.icon(gtx, "search", st.Icon, b.p.Sec) }),
			hgap(st.Gap),
			layout.Flexed(1, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Stack{Alignment: layout.W}.Layout(gtx,
					layout.Stacked(func(gtx C) D {
						if s.ed.Len() > 0 {
							return D{}
						}
						return b.label(gtx, 15, wReg, b.p.Muted, s.ph)
					}),
					layout.Stacked(func(gtx C) D { return b.th.Editor(gtx, &s.ed, 15, b.p.Text, b.p.Mention) }),
				)
			}),
			layout.Rigid(func(gtx C) D {
				if clr == nil || s.ed.Len() == 0 {
					return D{}
				}
				return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
					return clr.Layout(gtx, func(gtx C) D {
						pointerCursor(gtx)
						return b.label(gtx, 13, wReg, b.p.Sec, "Clear")
					})
				})
			}),
		)
	})
	return D{Size: image.Pt(w, hgt)}
}

// dirPress remembers where the last pointer press landed, in the view's
// coordinates, to anchor popovers (OpenProfile). begin registers a
// pass-through handler that is an ancestor of the whole view, so it sees
// presses that land on any child; Pop the returned stack at the end of
// Layout.
type dirPress struct{ at image.Point }

func (t *dirPress) begin(gtx C) clip.Stack {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: t, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			t.at = e.Position.Round()
		}
	}
	st := clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops)
	p := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	p.Pop()
	return st
}

// dirDropdown is an inline select with a Slack menu (w 280, bg surface,
// radius 8, padding 8px 0, 28px rows of 15px text; the checked item is
// #1264a3 with a check at the left; hover fills #1264a3 with white text).
type dirDropdown struct {
	labels  []string
	sel     int
	open    bool
	btn     widget.Clickable
	catcher widget.Clickable
	panel   widget.Clickable
	items   []widget.Clickable
}

// update handles clicks and reports whether the selection changed.
func (dd *dirDropdown) update(gtx C) bool {
	if len(dd.items) != len(dd.labels) {
		dd.items = make([]widget.Clickable, len(dd.labels))
	}
	if dd.btn.Clicked(gtx) {
		dd.open = !dd.open
	}
	if dd.catcher.Clicked(gtx) {
		dd.open = false
	}
	dd.panel.Clicked(gtx)
	changed := false
	for i := range dd.items {
		if dd.items[i].Clicked(gtx) {
			dd.open = false
			if dd.sel != i {
				dd.sel, changed = i, true
			}
		}
	}
	return changed
}

// layoutSort is the directory sort select: h 28, padding 0 12, radius 4,
// 1px #5e5d60@.45, 13px bold, a caret 4px after the label. The menu opens
// right-aligned under it.
func (dd *dirDropdown) layoutSort(b *base, gtx C, label string) D {
	dims := dd.btn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := b.th.Measure(gtx, 13, wBold, label)
		h, pad, chev := gtx.Dp(28), gtx.Dp(12), gtx.Dp(16)
		sz := image.Pt(pad+tw+gtx.Dp(4)+chev+pad, h)
		r := image.Rectangle{Max: sz}
		rad := gtx.Dp(4)
		rrect(gtx, r, rad, b.p.Surface)
		if dd.btn.Hovered() || dd.open {
			rrect(gtx, r, rad, b.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.BorderStr)
		dirAt(gtx, image.Pt(pad, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(tw, h))
			return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wBold, b.p.Text, label) })
		})
		dirAt(gtx, image.Pt(pad+tw+gtx.Dp(4), (h-chev)/2), func(gtx C) D { return b.icon(gtx, "caret-down", 16, b.p.Sec) })
		return D{Size: sz}
	})
	if dd.open {
		dd.menu(b, gtx, dims.Size, true)
	}
	return dims
}

// layoutSelect is a form select: w x 36, radius 8, 1px #7c7a7f, padding
// 0 12, 15px value, chevron at the right. The menu opens left-aligned.
func (dd *dirDropdown) layoutSelect(b *base, gtx C, width unit.Dp) D {
	if len(dd.labels) == 0 {
		return D{}
	}
	label := dd.labels[min(dd.sel, len(dd.labels)-1)]
	dims := dd.btn.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h, pad, chev := gtx.Dp(width), gtx.Dp(36), gtx.Dp(12), gtx.Dp(16)
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		rrect(gtx, r, rad, b.p.Surface)
		if dd.btn.Hovered() || dd.open {
			rrect(gtx, r, rad, b.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Faint)
		dirAt(gtx, image.Pt(pad, 0), func(gtx C) D {
			gtx.Constraints = layout.Exact(image.Pt(max(0, w-2*pad-chev-gtx.Dp(4)), h))
			return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 15, wReg, b.p.Text, label) })
		})
		dirAt(gtx, image.Pt(w-pad-chev, (h-chev)/2), func(gtx C) D { return b.icon(gtx, "chevron-down", 16, b.p.Sec) })
		return D{Size: r.Max}
	})
	if dd.open {
		dd.menu(b, gtx, dims.Size, false)
	}
	return dims
}

// menu records the open menu and defers it so it draws above everything;
// a full-window catcher under it closes the menu on an outside click.
func (dd *dirDropdown) menu(b *base, gtx C, btn image.Point, right bool) {
	if len(dd.items) != len(dd.labels) {
		return
	}
	m := op.Record(gtx.Ops)
	func() {
		big := 1 << 14
		defer op.Offset(image.Pt(-big, -big)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(2*big, 2*big))
		dd.catcher.Layout(g, func(gtx C) D { return D{Size: gtx.Constraints.Min} })
	}()
	itemH, padY := gtx.Dp(28), gtx.Dp(8)
	w := gtx.Dp(280)
	h := 2*padY + len(dd.labels)*itemH
	x := 0
	if right {
		x = btn.X - w
	}
	func() {
		defer op.Offset(image.Pt(x, btn.Y+gtx.Dp(4))).Push(gtx.Ops).Pop()
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		theme.Shadow(gtx, r, rad, gtx.Dp(12), argb(0x1f000000))
		rrect(gtx, r, rad, b.p.Surface)
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.Ring)
		g := gtx
		g.Constraints = layout.Exact(r.Max)
		dd.panel.Layout(g, func(gtx C) D { return D{Size: r.Max} })
		for i, l := range dd.labels {
			dirAt(gtx, image.Pt(0, padY+i*itemH), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(w, itemH))
				return dd.items[i].Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					fg := b.p.Text
					if i == dd.sel {
						fg = b.p.Link
					}
					if dd.items[i].Hovered() {
						fill(gtx, image.Rect(0, 0, w, itemH), b.p.Link)
						fg = rgb(0xffffff)
					}
					if i == dd.sel {
						dirAt(gtx, image.Pt(gtx.Dp(4), (itemH-gtx.Dp(16))/2), func(gtx C) D { return b.icon(gtx, "check", 16, fg) })
					}
					dirAt(gtx, image.Pt(gtx.Dp(24), 0), func(gtx C) D {
						gtx.Constraints = layout.Exact(image.Pt(w-gtx.Dp(48), itemH))
						return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 15, wReg, fg, l) })
					})
					return D{Size: image.Pt(w, itemH)}
				})
			})
		}
	}()
	op.Defer(gtx.Ops, m.Stop())
}
