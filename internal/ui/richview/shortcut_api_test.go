package richview

import (
	"image"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// wfRec records share_shortcut card actions.
type wfRec struct {
	recHandler
	acts  []string
	rects []image.Rectangle
}

func (h *wfRec) WorkflowCard(act string, c *rt.ShortcutCard, r image.Rectangle) {
	h.acts = append(h.acts, act+" "+c.TriggerID+" "+c.URL)
	h.rects = append(h.rects, r)
}

// workflowActRect clicks the card's target for act at anchor and returns
// the rectangle the handler got (the menu's anchor for "more").
func workflowActRect(v *View, act string, anchor image.Point) (image.Rectangle, bool) {
	for i, tg := range v.d.targets {
		if tg.kind == tWorkflowCard && tg.id == act {
			h := &wfRec{}
			v.activate(int16(i), anchor, h)
			if len(h.rects) != 1 {
				return image.Rectangle{}, true
			}
			return h.rects[0], true
		}
	}
	return image.Rectangle{}, false
}

// workflowAct clicks the card's target for act ("open", "start",
// "more") and returns what the handler heard; ok is false without such a
// target. Tests reach the card's API only through this file, so the
// red-evidence commit can stub it.
func workflowAct(v *View, act string) (string, bool) {
	for i, tg := range v.d.targets {
		if tg.kind == tWorkflowCard && tg.id == act {
			h := &wfRec{}
			v.activate(int16(i), image.Point{}, h)
			if len(h.acts) != 1 {
				return "", true
			}
			return h.acts[0], true
		}
	}
	return "", false
}
