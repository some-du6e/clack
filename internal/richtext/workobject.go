package richtext

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/workobject"
)

// WorkObject is a work object attachment: Slack's MessageAttachmentWorkObject
// (boot-shared AwtQ), which MessageKitAttachments (jWdN) draws for every
// attachment getWorkObjectsFromMessageAttachments (BH+8) finds a work
// object on. The card itself is ExternalWorkObjectEntity (K3AG), which
// WorkObjectEntity (u5Hu) picks for external work objects with layouts.
type WorkObject struct {
	E *workobject.Entity
	// AppUnfurlURL is the attachment's app_unfurl_url (the Open button's
	// link for apps whose unfurl URL is the canonical one).
	AppUnfurlURL string
	// Rich is each layout's rich_text and unknown fields as blocks, by
	// layout key and field index (WorkObjectRichText).
	Rich map[string][][]Block
	// Members are the Slack members the fields name (user and
	// slack_user values, the perspective user): their token's name
	// (forceRealName: the real name) and picture.
	Members map[string]WorkObjectMember
	// Channels are channel fields' names, without "#".
	Channels map[string]string
}

// WorkObjectMember is a member a work object names.
type WorkObjectMember struct {
	Name, Avatar string
}

// RealNamer is an optional Resolver extension: a member's real name
// (profile.real_name; the display name when it has none), which Slack's
// member tokens print when forceRealName is set.
type RealNamer interface {
	MemberRealName(id string) string
}

// Member returns what the card prints for member id.
func (w *WorkObject) Member(id string) WorkObjectMember {
	if w == nil {
		return WorkObjectMember{}
	}
	if m, ok := w.Members[id]; ok {
		return m
	}
	return WorkObjectMember{Name: id}
}

// Channel returns channel id's name (without "#").
func (w *WorkObject) Channel(id string) string {
	if w != nil {
		if n, ok := w.Channels[id]; ok && n != "" {
			return n
		}
	}
	return id
}

func (c *builder) woMember(w *WorkObject, id string) {
	if id == "" {
		return
	}
	if _, ok := w.Members[id]; ok {
		return
	}
	if w.Members == nil {
		w.Members = map[string]WorkObjectMember{}
	}
	m := WorkObjectMember{}
	if rn, ok := c.r.(RealNamer); ok {
		m.Name = rn.MemberRealName(id)
	}
	if m.Name == "" {
		if mn, ok := c.r.(MemberNamer); ok {
			m.Name, _ = mn.MemberName(id)
		}
	}
	if m.Name == "" {
		m.Name = c.r.UserName(id)
	}
	if mn, ok := c.r.(MemberNamer); ok {
		m.Avatar = mn.MemberAvatar(id)
	}
	w.Members[id] = m
}

// RichField returns the blocks of field i in layout key (nil when the
// field has no rich text).
func (w *WorkObject) RichField(key string, i int) []Block {
	if w == nil || w.Rich == nil {
		return nil
	}
	fs := w.Rich[key]
	if i < 0 || i >= len(fs) {
		return nil
	}
	return fs[i]
}

// workObjectOf is getWorkObject (BOLE n9: work_object_entity, else
// work_object) through WorkObjectModel.create: nil unless the attachment
// carries an external work object with layouts (5xtd W). Tableau work
// objects (WorkObjectEntity's other branch) need Tableau, which Clack
// has no client for, so they draw nothing, as an unrenderable entity
// does in Slack.
func workObjectOf(a *slack.Attachment) *workobject.Entity {
	raw := a.WorkObjectEntity
	if len(raw) == 0 || string(raw) == "null" {
		raw = a.WorkObject
	}
	if len(raw) == 0 {
		return nil
	}
	e := workobject.Parse(raw)
	if e == nil || !e.IsExternal() {
		return nil
	}
	return e
}

// IsWorkObject reports an attachment that carries a work object
// (BOLE isWorkObjectUnfurl), external or not.
func IsWorkObject(a *slack.Attachment) bool {
	return len(a.WorkObjectEntity) > 0 && string(a.WorkObjectEntity) != "null" ||
		len(a.WorkObject) > 0 && string(a.WorkObject) != "null"
}

// workObject converts a work object attachment's entity: its rich text
// fields become blocks once, here, so richview only draws them.
func (c *builder) workObject(a *slack.Attachment, e *workobject.Entity) *WorkObject {
	w := &WorkObject{E: e, AppUnfurlURL: SafeURL(a.AppUnfurlURL)}
	for _, key := range []string{workobject.Compact, workobject.Expanded, workobject.Full, workobject.Minimal} {
		l := e.Layout(key)
		if l == nil {
			continue
		}
		var rich [][]Block
		for i := range l.Fields {
			f := &l.Fields[i]
			for _, u := range f.Users {
				if u.UserType == "slack" {
					c.woMember(w, u.UserID)
				}
			}
			for _, ch := range f.Channels {
				if w.Channels == nil {
					w.Channels = map[string]string{}
				}
				w.Channels[ch.ChannelID] = trimHash(c.r.ChannelName(ch.ChannelID))
			}
			if (f.IsRichText() || f.IsUnknown()) && len(f.RichText) > 0 && string(f.RichText) != "null" {
				if rich == nil {
					rich = make([][]Block, len(l.Fields))
				}
				rich[i] = c.blocks(parseBlocks(json.RawMessage("[" + string(f.RichText) + "]")))
			}
		}
		if rich != nil {
			if w.Rich == nil {
				w.Rich = map[string][][]Block{}
			}
			w.Rich[key] = rich
		}
	}
	c.woMember(w, e.PerspectiveUserID())
	return w
}

func trimHash(s string) string {
	if len(s) > 0 && s[0] == '#' {
		return s[1:]
	}
	return s
}

// WorkObjectFromEntity is workObject for an entity that didn't come in
// an attachment (workObjects.getEntity's details, search results): the
// card and the flexpane draw it the same way.
func WorkObjectFromEntity(r Resolver, e *workobject.Entity, appUnfurlURL string) *WorkObject {
	if e == nil {
		return nil
	}
	c := &builder{r: r}
	return c.workObject(&slack.Attachment{AppUnfurlURL: appUnfurlURL}, e)
}
