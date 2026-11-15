package richtext

import (
	"encoding/json"
	"strings"
	"sync/atomic"
)

// Workflow mentions, 1:1 with Slack's web client (boot-shared jp/6
// WorkflowMention, build c07ab895506f, 2026-11-15). A rich_text
// "workflow_mention" element ({function_trigger_id, workflow_id, text,
// url, style, channel_id?, ts?}) is a RichTextSlug (Pc7Q), the slug the
// canvas and list mentions draw, with getWorkflowSlugAttributes' (Po6V)
// label and icon:
//
//   - The trigger comes from the triggers store (SzL7 bn with fetch:
//     workflows.triggers.info {trigger_id, include_workflow_details}; a
//     failed lookup is stored as {id, error}). Its variant (y1oE ak):
//     not_loaded while it hasn't come, normal for a trigger without an
//     error, hidden for access_denied, deleted for any other error.
//   - Labels: the workflow's title (trigger.workflow.title, else the
//     element's text) through sanitizeOpInsertString (I4Vw ti) for normal
//     and hidden; "Deleted workflow" for deleted.
//   - Icons (workflowSlugIconResolver): play, lock (hidden), trash
//     (deleted). Every variant but normal is disabled: no click.
//   - Slack draws nothing until the trigger has come (isLoading, the
//     empty o.A), and nothing for an element whose type isn't
//     workflow_mention.
//   - A click (normal) is openWorkflowDetailsModalCircular (aAr+): the
//     workflow details view. The slug's href is the element's url.
//
// Left out: the task variant (a workflow whose creation_source_type is a
// Slackbot task, behind a feature check: the "ai-workflow" icon, "Deleted
// task" and the task route) and the hover card (renderWorkflowHoverCard
// Trigger, WorkflowHoverCard).

// bkWorkflow is a workflow_mention element's own field (bk embeds it).
type bkWorkflow struct {
	FunctionTriggerID string `json:"function_trigger_id"`
}

// WorkflowSlug is a mention's trigger as the triggers store has it.
type WorkflowSlug struct {
	Title string // trigger.workflow.title ("" falls back to the element's text)
	Error string // the lookup's error ("" for a trigger that loaded)
}

// WorkflowSlugResolver resolves a mention's trigger; known is false
// while it hasn't come.
type WorkflowSlugResolver func(triggerID string) (s WorkflowSlug, known bool)

var workflowSlugs atomic.Pointer[WorkflowSlugResolver]

// SetWorkflowSlugs installs the mentions' resolver (the ui root's,
// workflows_wire.go: fetched once per trigger, the visible messages
// rebuilt when one lands; core builds messages on its own goroutines).
// Without one (nil) a mention is its element's text.
func SetWorkflowSlugs(f WorkflowSlugResolver) {
	if f == nil {
		workflowSlugs.Store(nil)
		return
	}
	workflowSlugs.Store(&f)
}

// WorkflowSlugVariant is getWorkflowSlugAttributes' variant for s.
func WorkflowSlugVariant(s WorkflowSlug, known bool) string {
	switch {
	case !known:
		return "not_loaded"
	case s.Error == "":
		return "normal"
	case s.Error == "access_denied":
		return "hidden"
	}
	return "deleted"
}

// workflowSlugIcon is workflowSlugIconResolver.
func workflowSlugIcon(variant string) string {
	switch variant {
	case "hidden":
		return "lock"
	case "deleted":
		return "trash"
	}
	return "play"
}

// sanitizeSlugText is sanitizeOpInsertString (I4Vw ti): line and
// paragraph separators become newlines, zero-width spaces and BOMs at
// the ends of each line go, soft hyphens go and no-break spaces become
// spaces.
func sanitizeSlugText(s string) string {
	s = strings.NewReplacer("\u2028", "\n", "\u2029", "\n").Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Trim(l, "\u200b\ufeff")
	}
	s = strings.Join(lines, "\n")
	return strings.NewReplacer("\u00ad", "", "\u00a0", " ").Replace(s)
}

// workflowMentionSpan is WorkflowMention for e: a Canvas-kind span (the
// shared RichTextSlug) with the trigger id as ID. ok is false while the
// trigger hasn't come.
func (c *builder) workflowMentionSpan(e *bk) (Span, bool) {
	text := e.textString()
	res := workflowSlugs.Load()
	if res == nil || e.FunctionTriggerID == "" {
		return Span{Kind: Text, Text: text}, text != ""
	}
	s, known := (*res)(e.FunctionTriggerID)
	variant := WorkflowSlugVariant(s, known)
	if variant == "not_loaded" {
		return Span{}, false
	}
	label := "Deleted workflow"
	if variant != "deleted" {
		label = sanitizeSlugText(firstNonEmpty(s.Title, text))
	}
	// the label is one line in the slug (a newline would break the pill)
	label = strings.ReplaceAll(label, "\n", " ")
	sp := Span{Kind: Canvas, ID: e.FunctionTriggerID, URL: e.URL, Icon: workflowSlugIcon(variant), Text: label,
		Parts: &SpanParts{Spans: []Span{{Kind: Text, Text: label}}}}
	if variant != "normal" {
		sp.Off = true // isDisabled: no onClick
	}
	return sp, true
}

// WorkflowMentionTrigger finds the workflow_mention behind a click on
// url in a message's blocks: its trigger id, or "" when no mention links
// there.
func WorkflowMentionTrigger(blocks []byte, url string) string {
	if len(blocks) == 0 || url == "" {
		return ""
	}
	var bs []bk
	if json.Unmarshal(blocks, &bs) != nil {
		return ""
	}
	var walk func([]bk) string
	walk = func(es []bk) string {
		for i := range es {
			e := &es[i]
			if e.Type == "workflow_mention" && e.URL == url && e.FunctionTriggerID != "" {
				return e.FunctionTriggerID
			}
			if id := walk(e.Elements); id != "" {
				return id
			}
		}
		return ""
	}
	return walk(bs)
}
