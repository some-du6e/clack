package views

import (
	"sort"

	"gioui.org/layout"
	"gioui.org/unit"
)

// The Files & links tab's peek: FilesTabPeek (boot-shared q5Cv, build
// 3889f0c93d30) is ChannelTabPeek around files_tab_view with isPreview
// (3ztk: .files_tab_view__preview, clogUiPage channel_tabs_files_peek, no
// inputId so no search), whose rows useChannelAttachmentsExplorer builds
// with isPreview:
//
//   - every browser done and empty (isShowingEmptyPeekPlaceholder): the
//     "zero-empty" state, "No files or links found for this channel.";
//   - any browser loading: the list-loading fragment;
//   - else no controls and no media strip: getChannelAttachmentsMixedRows
//     with excludeMediaFromList (no search), so the files and the links by
//     share time, newest first, links with simpleBorder, and no "More
//     results by type".
//
// The peek's contentHeight is 200 for the placeholder, else the rows'
// heights (section-label 32, expandable-grid 90, file results 61, links
// 63, list-loading 420) plus 10, at most 600.

// NewChannelFilesPeek is ch's Files & links tab as its peek draws it.
func NewChannelFilesPeek(h Host, ch string) *ChannelFiles {
	v := NewChannelFiles(h, ch)
	v.preview = true
	return v
}

// previewRows are useChannelAttachmentsExplorer's rows with isPreview.
func (v *ChannelFiles) previewRows() []cfRow {
	if v.peekPlaceholder() {
		return []cfRow{{key: "zero-empty", kind: cfrEmpty, empty: nsTabbed.T("No files or links found for this channel.")}}
	}
	if v.media.loading() || v.files.loading() || v.links.loading() {
		return []cfRow{{key: "list-loading", kind: cfrLoading}}
	}
	var mixed []cfMixed
	for i := range v.files.items {
		mixed = append(mixed, cfMixed{file: &v.files.items[i], ts: fileShareTS(&v.files.items[i])})
	}
	for i := range v.links.links {
		mixed = append(mixed, cfMixed{link: &v.links.links[i], ts: v.links.links[i].Timestamp})
	}
	sort.SliceStable(mixed, func(i, j int) bool { return mixed[i].ts > mixed[j].ts })
	if len(mixed) == 0 {
		return nil // media only: excluded from the list, and no empty state while there are media
	}
	rows := make([]cfRow, 0, len(mixed))
	for i, m := range mixed {
		r := cfRow{first: i == 0, last: i == len(mixed)-1}
		if m.file != nil {
			r.key, r.kind, r.file = m.file.ID, cfrFile, m.file
		} else {
			r.key, r.kind, r.link = "link:"+m.link.URL, cfrLink, m.link
		}
		rows = append(rows, r)
	}
	return rows
}

// peekPlaceholder is isShowingEmptyPeekPlaceholder: the three browsers
// done and empty.
func (v *ChannelFiles) peekPlaceholder() bool {
	return !v.media.loading() && len(v.media.items) == 0 && !v.files.loading() && len(v.files.items) == 0 &&
		!v.links.loading() && len(v.links.links) == 0
}

// PeekHeight is FilesTabPeek's contentHeight in px.
func (v *ChannelFiles) PeekHeight(gtx layout.Context) int {
	v.ensureLoaded()
	if v.peekPlaceholder() {
		return gtx.Dp(200)
	}
	h := 0
	for _, r := range v.previewRows() {
		switch r.kind {
		case cfrFile:
			h += 61
		case cfrLink:
			h += 63
		case cfrLoading:
			h += 420
		}
	}
	return gtx.Dp(unit.Dp(min(h+10, 600)))
}

// PeekRows are the preview's row kinds and keys (tests).
func (v *ChannelFiles) PeekRows() []string {
	var out []string
	for _, r := range v.previewRows() {
		switch r.kind {
		case cfrFile:
			out = append(out, "file:"+r.key)
		case cfrLink:
			out = append(out, r.key)
		case cfrLoading:
			out = append(out, "loading")
		case cfrEmpty:
			out = append(out, "empty:"+r.empty)
		}
	}
	return out
}

// ensureLoaded starts the browsers (the view's mount) and takes their
// answers, so PeekHeight reads this frame's rows before Layout runs.
func (v *ChannelFiles) ensureLoaded() {
	v.fv.drainDone()
	if !v.started {
		v.started = true
		v.Refresh()
	}
	v.media.apply(v.fv)
	v.files.apply(v.fv)
	v.links.apply()
}
