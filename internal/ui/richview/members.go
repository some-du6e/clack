package richview

import "sync/atomic"

// User mentions name the member as plugins present it. Slack's mention
// (the rich text renderer's user element, MemberSlug inside MemberLink)
// reads the member from the store when it renders, so a plugin's patch
// over the members slice (Taut's and Slick's Nicknames) changes what every
// mention says; Clack resolves "@name" when it parses a message, so the
// layout asks MemberLabel for each mention instead, and lays the doc out
// again when a label it used changes.

// MemberLabel returns the name a user mention shows as plugins present
// member id (Slack's display rule over the patched member), ok false when
// no plugin changes it. The ui root installs it while a plugin runtime is
// attached; it must be cheap and never block (taut's patched views).
type MemberLabel func(id string) (name string, ok bool)

var memberLabel atomic.Pointer[MemberLabel]

// SetMemberLabel installs (or with nil removes) the label hook.
func SetMemberLabel(f MemberLabel) {
	if f == nil {
		memberLabel.Store(nil)
		return
	}
	memberLabel.Store(&f)
}

func memberLabelFn() MemberLabel {
	if p := memberLabel.Load(); p != nil {
		return *p
	}
	return nil
}

// nameUse is a user mention a layout drew and the plugin label it used
// ("" for the parsed name).
type nameUse struct {
	id, name string
}

// mentionText is a user mention's text: "@" and the plugin's label when a
// plugin changes the member, else the parsed text. Each use is recorded so
// namesChanged can tell when to lay out again.
func (l *lay) mentionText(id, parsed string) string {
	f := memberLabelFn()
	if f == nil || id == "" {
		return parsed
	}
	n, ok := f(id)
	if !ok {
		n = ""
	}
	l.d.names = append(l.d.names, nameUse{id: id, name: n})
	if n == "" {
		return parsed
	}
	return "@" + n
}

// namesChanged reports a label the last layout used that reads
// differently now (a nickname set or cleared, the patched member loaded).
func (v *View) namesChanged() bool {
	if len(v.d.names) == 0 {
		return false
	}
	f := memberLabelFn()
	for _, u := range v.d.names {
		n := ""
		if f != nil {
			if s, ok := f(u.id); ok {
				n = s
			}
		}
		if n != u.name {
			return true
		}
	}
	return false
}
