package views

// Slack component boundaries Slick plugins patch in the views
// (docs/PLUGINS-components.md §9: BetterBlock, CallTimer, LetMeSee,
// BetterHuddles). Same rules as slackc.go: one atomic load when nothing
// is patched, props built only under a patch, and natives that honour the
// props a plugin hands to <Original>. A plugin that renders null in place
// of a boundary hides that part of the view (BetterBlock's blocked people).

import (
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// ---- Activity: the parts of an ActivityItem ----
//
//	ActivityItem
//	└ ActivityThread (thread items) | ActivityDM (DM items) | —
//	  └ ActivityRowContentMultiLine (Detailed) | ActivityRowContentSingleLine (Dense)
//	    └ ActivityRowContentDetailed | ActivityRowContentDense
//	      └ ActivityRowContentSenderAvatarLayout, ActivityPrivacyBlurredName, …

// msg is the message the row previews: an ActivityThread override, or the
// item's own.
func (r *actRow) msg() *core.Message {
	if r.pm != nil {
		return r.pm
	}
	return r.it.Msg
}

// isThread reports a thread item (Slack's thread / thread_v2 rows).
func (r *actRow) isThread() bool {
	return r.it.ThreadTS != "" && (r.it.Type == "thread_v2" || r.it.Type == "thread")
}

func (r *actRow) rowKind(gtx C) D {
	switch {
	case r.isThread():
		return slackc.Component(gtx, "ActivityThread", r, (*actRow).threadProps, (*actRow).layoutThread)
	case r.d.dm:
		return slackc.Component(gtx, "ActivityDM", r, (*actRow).dmProps, (*actRow).layoutDM)
	}
	return r.lines(gtx)
}

// rowBase is the props every inner activity boundary gets.
func (r *actRow) rowBase(part string) slackc.Props {
	return slackc.Props{"$key": r.key + "/" + part, "activityItem": r.itemShape(), "msg": msgProps(r.msg()),
		"density": r.density(), "isDmTab": r.v.dmOnly()}
}

// threadProps: ActivityThread {activityItem, msg, messageTs, minUnreadTs,
// unreadCount}.
func (r *actRow) threadProps() slackc.Props {
	p := r.rowBase("thread")
	p["messageTs"] = r.it.TS
	p["threadTs"] = r.it.ThreadTS
	p["unreadCount"] = r.it.Count
	return p
}

// layoutThread honours BetterBlock's projection of a thread item: msg
// replaced by the latest visible message of the thread (its sender and
// preview), and unreadCount without the hidden replies.
func (r *actRow) layoutThread(gtx C, p slackc.Props) D {
	if p != nil {
		r.threadOverride(p)
	}
	return r.lines(gtx)
}

func (r *actRow) threadOverride(p slackc.Props) {
	if n, ok := p["unreadCount"].(float64); ok {
		r.count = max(0, int(n))
	} else if ai := p.Map("activityItem"); ai != nil {
		if n, ok := ai["unreadCount"].(float64); ok {
			r.count = max(0, int(n))
		}
	}
	mp := p.Map("msg")
	it := r.it
	if mp == nil || it.Msg == nil || r.d.invite || it.Reaction != nil {
		return
	}
	src := it.Msg
	if ts := mp.StrOr("ts", src.TS); ts != src.TS {
		if m := r.threadMsg(ts); m != nil {
			src, r.pm = m, m
		}
	}
	om := msgOverride(src, mp)
	r.pd = *r.cur
	if n, av := r.v.authorOf(&om); n != "" {
		r.pd.name, r.pd.avatar = n, av
	}
	r.cur = &r.pd
}

// dmProps: ActivityDM {channelId, activityItem, msg, isDmTab}.
func (r *actRow) dmProps() slackc.Props {
	p := r.rowBase("dm")
	p["channelId"] = r.it.Channel
	return p
}

func (r *actRow) layoutDM(gtx C, _ slackc.Props) D { return r.lines(gtx) }

// lines is the row layout for the density: Slack's MultiLine row holds
// the Detailed content, its SingleLine row the Dense one.
func (r *actRow) lines(gtx C) D {
	if r.v.dense {
		return slackc.Component(gtx, "ActivityRowContentSingleLine", r, (*actRow).singleProps, (*actRow).layoutSingle)
	}
	return slackc.Component(gtx, "ActivityRowContentMultiLine", r, (*actRow).multiProps, (*actRow).layoutMulti)
}

// multiProps: ActivityRowContentMultiLine {activityItem, itemId, index,
// hasFocus, isFirstEntry, precedesHeader}.
func (r *actRow) multiProps() slackc.Props {
	p := r.rowBase("multi")
	p["itemId"], p["hasFocus"], p["isFirstEntry"], p["precedesHeader"] = r.key, false, false, false
	return p
}

func (r *actRow) singleProps() slackc.Props {
	p := r.multiProps()
	p["$key"] = r.key + "/single"
	return p
}

func (r *actRow) layoutMulti(gtx C, _ slackc.Props) D {
	return slackc.Component(gtx, "ActivityRowContentDetailed", r, (*actRow).detailedProps, (*actRow).layoutDetailed)
}

func (r *actRow) layoutSingle(gtx C, _ slackc.Props) D {
	return slackc.Component(gtx, "ActivityRowContentDense", r, (*actRow).denseProps, (*actRow).layoutDense)
}

// detailedProps / denseProps: ActivityRowContentDetailed|Dense
// {activityItem, density, msg}.
func (r *actRow) detailedProps() slackc.Props { return r.rowBase("detailed") }
func (r *actRow) denseProps() slackc.Props    { return r.rowBase("dense") }

func (r *actRow) layoutDetailed(gtx C, _ slackc.Props) D { return r.v.detailedContent(gtx, r) }
func (r *actRow) layoutDense(gtx C, _ slackc.Props) D    { return r.v.denseContent(gtx, r) }

// threadMsg is the thread message ts an ActivityThread override points
// at: the root, or a cached reply (converted once, kept on the row).
func (r *actRow) threadMsg(ts string) *core.Message {
	it := r.it
	if it.Root != nil && it.Root.TS == ts {
		return it.Root
	}
	if r.tm != nil && r.tm.TS == ts && r.tm.Channel == it.Channel {
		return r.tm
	}
	a := r.v.h.Core()
	sm, ok := a.S.Find(it.Channel, ts)
	if !ok {
		return nil
	}
	m := a.MessageFromSlack(it.Channel, &sm)
	r.tm = &m
	return r.tm
}
