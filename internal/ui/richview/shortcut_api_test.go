package richview

import (
	"image"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// wfRec records share_shortcut card actions.
type wfRec struct {
	recHandler
	acts []string
}

func (h *wfRec) WorkflowCard(act string, c *rt.ShortcutCard, _ image.Rectangle) {
	h.acts = append(h.acts, act+" "+c.TriggerID+" "+c.URL)
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
