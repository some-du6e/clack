package richtext

import (
	"encoding/json"
	"net/url"
)

// List slugs, 1:1 with Slack's web client (CDN build fa938a086569,
// 2026-11-13):
//
//   - A rich_text "list_record" element ({file_id, record_id?, view_id?,
//     url?, style?}: a pasted list, list view or list item link) is
//     Slack's ListRecord (boot-shared 0MEu): the same RichTextSlug (Pc7Q)
//     as the canvas slug, with getListSlugAttributes' (QFv5) label and
//     icon. Its variant (listSlugVariantResolver) is the list's
//     getFileStatus unless that is "normal" and the element names a
//     record or a view: "not_loaded" while the record is unknown,
//     "record" / "record-deleted" / "record-missing", "view" /
//     "view-deleted", else "normal".
//   - Labels: the list's title ("normal"), getFileStatusStringByType's
//     "Deleted list", "Private list" or "Access denied" (deleted, hidden,
//     locked, missing), the record's primary field (its rich text cut at
//     25 characters with "…"; "Untitled item" when empty), "Deleted item",
//     "Item not found", "{listTitle}: {viewName}", "Deleted list view".
//   - Icons (listSlugIconResolver): trash (deleted, record-deleted,
//     view-deleted), lock (hidden, locked, missing), list-item (record,
//     record-missing, not_loaded), else lists. A slug whose variant says
//     deleted is disabled.
//   - Slack draws nothing until the list is in its lists store (`if (!p)
//     return null`) and a loading shimmer while the record isn't
//     (isLoading, "not_loaded"); Clack's resolver fetches both and the
//     message is rebuilt, drawing nothing meanwhile.
//   - A click opens the list focused on the record (openListFromSlug:
//     listViewId, focusRecordId); the slug's link is the element's url,
//     else the list's link with record_id / view_id.

// ListRecordResolver is an optional Resolver extension (store.Store): a
// list record's primary field for list slugs. state is "base", "deleted"
// (the record is gone) or "missing" (isNonExistent); known is false while
// it isn't fetched.
type ListRecordResolver interface {
	ListRecordName(fileID, recordID string) (name json.RawMessage, state string, known bool)
}

// FileChannel is a file channel that reads as its file
// (search_file_channel_messages): Kind "canvas" or "list".
type FileChannel struct {
	Kind, FileID string
}

// FileChannelResolver is an optional Resolver extension (store.Store):
// BaseMrkdwnChannel's fileIdForChannel / fileChannelType.
type FileChannelResolver interface {
	FileChannel(channelID string) (FileChannel, bool)
}

// listNameMax is getListSlugAttributes' record name length.
const listNameMax = 25

// listSlugIcon is listSlugIconResolver.
func listSlugIcon(variant string) string {
	switch variant {
	case "deleted", "record-deleted", "view-deleted":
		return "trash"
	case "hidden", "locked", "missing":
		return "lock"
	case "record", "record-missing", "not_loaded":
		return "list-item"
	}
	return "lists"
}

// listStatusLabel is getFileStatusStringByType for a list.
func listStatusLabel(status string) string {
	switch status {
	case "deleted":
		return "Deleted list"
	case "locked":
		return "Access denied"
	}
	return "Private list" // hidden, missing
}

// ListURL is the link a list slug opens: the element's url, else the
// list's docs link Clack routes itself, with record_id or view_id.
func ListURL(raw, fileID, recordID, viewID string) string {
	if u := SafeURL(raw); u != "" {
		return u
	}
	u := "https://app.slack.com/lists/-/" + fileID
	q := url.Values{}
	if recordID != "" {
		q.Set("record_id", recordID)
	} else if viewID != "" {
		q.Set("view_id", viewID)
	}
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// listRecordSpan is the ListRecord slug for element e (a Canvas-kind span:
// both are Slack's RichTextSlug). ok is false while the list isn't known
// (Slack draws nothing then).
func (c *builder) listRecordSpan(e *bk) (Span, bool) {
	cr, isCR := c.r.(CanvasResolver)
	if !isCR || e.FileID == "" {
		return Span{}, false
	}
	ref, known := cr.CanvasRef(e.FileID)
	if !known {
		return Span{}, false
	}
	sp := Span{Kind: Canvas, ID: e.FileID, URL: ListURL(e.URL, e.FileID, e.RecordID, e.ViewID)}
	status := ref.Status
	if status == "" {
		status = "normal"
	}
	variant := status
	var name json.RawMessage
	if status == "normal" && (e.RecordID != "" || e.ViewID != "") {
		recState, recKnown := "", true
		if e.RecordID != "" {
			recKnown = false
			if lr, ok := c.r.(ListRecordResolver); ok {
				name, recState, recKnown = lr.ListRecordName(e.FileID, e.RecordID)
			}
		}
		_, hasView := ref.Views[e.ViewID]
		switch {
		case e.RecordID != "" && !recKnown:
			variant = "not_loaded"
		case e.RecordID != "" && recState == "missing":
			variant = "record-missing"
		case e.RecordID != "" && recState == "base":
			variant = "record"
		case e.RecordID != "":
			variant = "record-deleted"
		case !hasView:
			variant = "view-deleted"
		default:
			variant = "view"
		}
	}
	if variant == "not_loaded" {
		return Span{}, false // isLoading: Slack's shimmer until the record arrives
	}
	title := titleSpans(c.r, ref.Title) // Name: the file's title with its :emoji:
	var parts []Span
	switch variant {
	case "normal":
		parts = title
	case "deleted", "hidden", "locked", "missing":
		parts = []Span{{Kind: Text, Text: listStatusLabel(variant)}}
	case "record":
		parts = c.listRecordName(name)
		if len(parts) == 0 {
			parts = []Span{{Kind: Text, Text: "Untitled item"}}
		}
	case "record-deleted":
		parts = []Span{{Kind: Text, Text: "Deleted item"}}
	case "record-missing":
		parts = []Span{{Kind: Text, Text: "Item not found"}}
	case "view":
		parts = append(append([]Span(nil), title...), Span{Kind: Text, Text: ": " + ref.Views[e.ViewID]})
	case "view-deleted":
		parts = []Span{{Kind: Text, Text: "Deleted list view"}}
	}
	sp.Icon = listSlugIcon(variant)
	switch variant {
	case "deleted", "record-deleted", "view-deleted":
		sp.Off = true // isDisabled: _.includes("deleted")
	}
	sp.Parts = &SpanParts{Spans: parts}
	sp.Text = PlainSpans(parts)
	return sp, true
}

// listRecordName is ListSlugRichTextLabel's record case: the primary
// field's rich_text block (RichTextBlockPreview, truncateLength 25), its
// first line.
func (c *builder) listRecordName(raw json.RawMessage) []Span {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var b bk
	if json.Unmarshal(raw, &b) != nil || b.Type != "rich_text" {
		var bs []bk // a field's rich_text is an array of blocks
		if json.Unmarshal(raw, &bs) != nil || len(bs) == 0 || bs[0].Type != "rich_text" {
			return nil
		}
		b = bs[0]
	}
	return cutRunes(firstLine(flatten(c.blocks([]bk{b}))), listNameMax)
}

// fileChannelSpan is renderFileChannelSlug for a file channel the
// resolver knows the file of: the canvas or list_record element with the
// file id alone.
func fileChannelSpan(r Resolver, id string) (Span, bool) {
	fr, ok := r.(FileChannelResolver)
	if !ok {
		return Span{}, false
	}
	fc, ok := fr.FileChannel(id)
	if !ok {
		return Span{}, false
	}
	b := &builder{r: r}
	if fc.Kind == "canvas" {
		return b.canvasSpan(&bk{Type: "canvas", FileID: fc.FileID}), true
	}
	return b.listRecordSpan(&bk{Type: "list_record", FileID: fc.FileID})
}
