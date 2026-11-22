package core

import (
	"encoding/json"
	"strconv"
	"strings"
)

// SearchMatches says which of a search result's attachments and files
// hold a match, as SearchResult (boot-shared IT2K, CDN build
// a09b625d90aa) tells from the answer's highlight markers before it
// strips them:
//
//   - attachmentHasSearchMatch: the attachment's extracts have a title,
//     pretext, text or fields, or a highlight start (U+E000) is in its
//     title, fallback, pretext, text, author_name, author_subname,
//     service_name or footer;
//   - fileHasSearchMatch: transcription_hl, file_contents_hl or
//     list_records_hl lines, contents_extracts, alt_text_highlights,
//     preview_highlight, contents_highlight or is_semantic_highlight, or
//     a highlight start in search_title, search_name, search_preview,
//     title or name.
//
// The card draws only the matched attachments and files unless nothing
// in the message matched at all (hasNoVisibleContent).
type SearchMatches struct {
	// Attachments holds the matched attachments by id ("idx:N", the
	// attachment's index, for one without an id); Files by file id.
	Attachments map[string]bool
	Files       map[string]bool
}

// AttachmentKey is the key SearchMatches.Attachments uses for an
// attachment with id (richtext.Attachment.ID, "" when it had none) at
// index i among the message's attachments.
func AttachmentKey(id string, i int) string {
	if id != "" {
		return id
	}
	return "idx:" + strconv.Itoa(i)
}

// searchMatches reads every result message's matches from a
// search.modules answer still holding its markers, keyed by item and
// message index.
func searchMatches(raw []byte) map[[2]int]SearchMatches {
	var res struct {
		Items []struct {
			Messages []struct {
				Attachments []map[string]any `json:"attachments"`
				Files       []map[string]any `json:"files"`
			} `json:"messages"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return nil
	}
	out := map[[2]int]SearchMatches{}
	for i, it := range res.Items {
		for j, m := range it.Messages {
			var sm SearchMatches
			for k, a := range m.Attachments {
				if !attachmentHasSearchMatch(a) {
					continue
				}
				if sm.Attachments == nil {
					sm.Attachments = map[string]bool{}
				}
				id := ""
				if n, ok := a["id"].(float64); ok && n != 0 {
					id = strconv.Itoa(int(n))
				}
				sm.Attachments[AttachmentKey(id, k)] = true
			}
			for _, f := range m.Files {
				id, _ := f["id"].(string)
				if id == "" || !fileHasSearchMatch(f) {
					continue
				}
				if sm.Files == nil {
					sm.Files = map[string]bool{}
				}
				sm.Files[id] = true
			}
			out[[2]int{i, j}] = sm
		}
	}
	return out
}

// markedString is eG.Ay: a string holding a highlight start.
func markedString(v any) bool {
	s, ok := v.(string)
	return ok && strings.ContainsRune(s, '')
}

// nonEmpty reports a non-empty array (or any other present, non-blank
// value, as JavaScript's ?.length / truthiness reads it).
func nonEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case []any:
		return len(x) > 0
	case string:
		return x != ""
	case bool:
		return x
	case float64:
		return x != 0
	}
	return true
}

func attachmentHasSearchMatch(a map[string]any) bool {
	if ex, ok := a["extracts"].(map[string]any); ok {
		for _, k := range []string{"title", "pretext", "text", "fields"} {
			if l, ok := ex[k].([]any); ok && len(l) > 0 {
				return true
			}
			if s, ok := ex[k].(string); ok && s != "" {
				return true
			}
		}
	}
	for _, k := range []string{"title", "fallback", "pretext", "text", "author_name", "author_subname", "service_name", "footer"} {
		if markedString(a[k]) {
			return true
		}
	}
	return false
}

func fileHasSearchMatch(f map[string]any) bool {
	for _, k := range []string{"transcription_hl", "file_contents_hl", "list_records_hl"} {
		if hl, ok := f[k].(map[string]any); ok && nonEmpty(hl["lines"]) {
			return true
		}
	}
	for _, k := range []string{"contents_extracts", "alt_text_highlights", "preview_highlight", "contents_highlight", "is_semantic_highlight"} {
		if nonEmpty(f[k]) {
			return true
		}
	}
	for _, k := range []string{"search_title", "search_name", "search_preview", "title", "name"} {
		if markedString(f[k]) {
			return true
		}
	}
	return false
}
