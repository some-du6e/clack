package richtext

import "strconv"

// shortcutOf describes a Doc's share_shortcut cards (shortcut.go) as
// "<state> <type> <title>|<description>|url=…|byline=…|icon=…|ai=…".
// withTriggerIcons installs a workflow slug resolver that knows every
// trigger with icon. Tests reach the card only through this file, so the
// red-evidence commit can stub it.
func shortcutOf(d Doc) []string {
	var out []string
	for _, b := range d.Blocks {
		if b.Kind != ShortcutBlock || b.Shortcut == nil {
			continue
		}
		s := b.Shortcut
		out = append(out, s.State+" "+s.TriggerType+" "+s.Title+"|"+s.Description+"|url="+s.URL+"|byline="+s.Byline+
			"|bicon="+s.BylineIcon+"|icon="+s.Icon+"|ai="+strconv.FormatBool(s.AI)+"|share="+s.ShareURL+"|wf="+s.WorkflowID)
	}
	return out
}

func withTriggerIcons(icon string) func() {
	SetWorkflowSlugs(func(string) (WorkflowSlug, bool) { return WorkflowSlug{Title: "t", Icon: icon}, true })
	return func() { SetWorkflowSlugs(nil) }
}
