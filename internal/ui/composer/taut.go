package composer

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// Taut plugin support for the composer (docs/PLUGINS-taut.md).
//
// The composer routes Slack's named actions through taut.Dispatch with
// Slack's argument shapes, so plugins that patch those thunks (SilentTyping,
// OwlMode, NativeAtChannel, AnonymizeFilenames, PrivateChannel…) see and
// change them exactly as in Slack. With no plugin patching a name, every
// call site takes its old direct path: one atomic load decides, and
// nothing is allocated (see BenchmarkThunkGuard / TestRoutingAllocs).
//
// The pattern is the same everywhere:
//
//	if t := currentTaut(); t.ThunkPatched(thX) {
//		… taut.Dispatch(ctx, t, thX, xArgs{…}, func(ctx, a xArgs) (R, error) { …native… })
//	} else {
//		…native, unchanged…
//	}
//
// The closure is only built inside the patched branch. A shared helper that
// took the native as a parameter would make the caller's closure escape
// (and allocate) even when nothing is patched.

// Slack's thunk names the composer dispatches.
const (
	thTyping         = "currentUserStartedTyping"
	thTypingEnded    = "currentUserEndedTyping"
	thPrepareAndSend = "prepareAndSendMessage"
	thSendMessage    = "sendMessage"
	thDraftCreate    = "syncDraftCreate"
	thDraftUpdate    = "syncDraftUpdates"
	thDraftDelete    = "deleteDraftApi"
	thPendingFile    = "addAndUploadPendingFile"
	thUploadFiles    = "uploadFiles"
	thACChannels     = "autocompleteChannels"
	thACMembers      = "autocompleteMembers"
)

var tautRef atomic.Pointer[taut.Taut]

// SetTaut connects the composer to the plugin system (the ui root calls it
// next to taut.New, and again after an account switch; nil disconnects).
func SetTaut(t *taut.Taut) {
	extendTaut(t)
	tautRef.Store(t)
}

// currentTaut is the plugin system, or nil. Every *taut.Taut method the
// composer uses on the hot path is nil-safe or guarded here.
func currentTaut() *taut.Taut { return tautRef.Load() }

// tautCtx bounds a synchronous JS round trip made on the UI goroutine
// (inline markup parsing, link transforms). Plugins answer in well under a
// millisecond; a wedged plugin must not freeze typing.
const tautSyncTimeout = 150 * time.Millisecond

// ---- Slack's argument shapes ----

type typingArgs struct {
	ChannelID string `json:"channelId"`
	ThreadTs  string `json:"threadTs,omitempty"`
}

// prepareSendArgs is prepareAndSendMessage's options object. Slack's
// composer passes the Quill delta; Clack also passes the blocks and the
// mrkdwn fallback it built from it, so plugins needn't convert.
type prepareSendArgs struct {
	ChannelID       string          `json:"channelId"`
	ReplyToTs       string          `json:"replyToTs,omitempty"`
	Delta           map[string]any  `json:"delta,omitempty"`
	Text            string          `json:"text"`
	Blocks          json.RawMessage `json:"blocks,omitempty"`
	ShouldBroadcast bool            `json:"shouldBroadcast,omitempty"`
	Broadcast       bool            `json:"broadcast,omitempty"`
	DraftID         string          `json:"draftId,omitempty"`
	ViewContext     string          `json:"viewContext,omitempty"`
	FileIDs         []string        `json:"fileIds,omitempty"`
}

// sendMessageArgs is the chat.postMessage-shaped sendMessage thunk.
type sendMessageArgs struct {
	Channel        string          `json:"channel"`
	Blocks         json.RawMessage `json:"blocks,omitempty"`
	Text           string          `json:"text,omitempty"`
	ThreadTs       string          `json:"thread_ts,omitempty"`
	ReplyBroadcast bool            `json:"reply_broadcast,omitempty"`
}

// draftSyncArgs is syncDraftCreate / syncDraftUpdates: the composer's
// draftId (Slack's "C…" or "C…-<threadTs>", which OwlMode parses) plus the
// draft. ID is the server draft id (Dr…), empty on create.
type draftSyncArgs struct {
	DraftID   string          `json:"draftId"`
	ID        string          `json:"id,omitempty"`
	ChannelID string          `json:"channelId"`
	ThreadTs  string          `json:"threadTs,omitempty"`
	Broadcast bool            `json:"broadcast,omitempty"`
	Ops       []any           `json:"ops,omitempty"`
	Blocks    json.RawMessage `json:"blocks,omitempty"`
	Text      string          `json:"text,omitempty"`
	FileIDs   []string        `json:"file_ids,omitempty"`
}

// draftResult is what the draft natives return (the saved draft's id).
type draftResult struct {
	ID string `json:"id"`
}

type draftRef struct {
	ID            string `json:"id"`
	ClientDraftID string `json:"client_draft_id,omitempty"`
}

// deleteDraftArgs is deleteDraftApi's {draft: {id, …}, reason?}.
type deleteDraftArgs struct {
	Draft  draftRef `json:"draft"`
	Reason string   `json:"reason,omitempty"`
}

// pendingFileArgs is addAndUploadPendingFile's {file}.
type pendingFileArgs struct {
	File taut.File `json:"file"`
}

// pendingFileResult is addAndUploadPendingFile's answer: Slack's fileIds,
// plus what the composer needs to share the file later.
type pendingFileResult struct {
	FileIDs []string `json:"fileIds"`
	Name    string   `json:"name,omitempty"`
	Title   string   `json:"title,omitempty"`
	Size    int64    `json:"size,omitempty"`
}

// uploadFilesArgs is uploadFiles' {files, channelId, …}: upload and share
// in one go (the composer's fallback for files not uploaded at attach).
type uploadFilesArgs struct {
	Files          []taut.File `json:"files"`
	ChannelID      string      `json:"channelId"`
	ThreadTs       string      `json:"threadTs,omitempty"`
	InitialComment string      `json:"initialComment,omitempty"`
	FileIDs        []string    `json:"fileIds,omitempty"` // already uploaded (shared with these)
}

// ---- typing ----

// sendTyping reports typing (currentUserStartedTyping; SilentTyping and
// OwlMode swallow it). Call off the UI goroutine.
func sendTyping(api coreAPI, ch, ts string) {
	if t := currentTaut(); t.ThunkPatched(thTyping) {
		_, _ = taut.Dispatch(context.Background(), t, thTyping, typingArgs{ChannelID: ch, ThreadTs: ts},
			func(_ context.Context, a typingArgs) (any, error) {
				return nil, typingNative(api, a.ChannelID, a.ThreadTs)
			})
		return
	}
	_ = typingNative(api, ch, ts)
}

// typingNative is currentUserStartedTyping's native (core.Typing over the
// realtime socket). A var so tests can observe it without a socket.
var typingNative = func(api coreAPI, ch, ts string) error { return api.typingErr(ch, ts) }

// ---- send ----

// transformBlocks applies onMessageSendBlocks to a send, edit or schedule
// and returns the mrkdwn fallback rebuilt from the new blocks when a plugin
// changed them (ClearURLs, InvisibleForward…).
func transformBlocks(ctx context.Context, text string, blocks json.RawMessage) (string, json.RawMessage) {
	t := currentTaut()
	if t == nil || !t.HasSendTransforms() || len(blocks) == 0 {
		return text, blocks
	}
	out := t.TransformSendBlocks(ctx, blocks)
	if string(out) == string(blocks) {
		return text, blocks
	}
	if d, err := DocFromBlocksJSON(out, nil); err == nil && d != nil {
		text = d.Mrkdwn()
	}
	return text, out
}

// postMessage is the send itself (Slack's sendMessage thunk, inside
// prepareAndSendMessage). It applies the send transforms first.
func postMessage(ctx context.Context, api coreAPI, ch, ts, text string, blocks json.RawMessage, broadcast bool) error {
	text, blocks = transformBlocks(ctx, text, blocks)
	if t := currentTaut(); t.ThunkPatched(thSendMessage) {
		_, err := taut.Dispatch(ctx, t, thSendMessage, sendMessageArgs{Channel: ch, Blocks: blocks, Text: text, ThreadTs: ts, ReplyBroadcast: broadcast},
			func(ctx context.Context, a sendMessageArgs) (any, error) {
				_, err := api.send(ctx, a.Channel, a.ThreadTs, a.Text, a.Blocks, a.ReplyBroadcast)
				return nil, err
			})
		return err
	}
	_, err := api.send(ctx, ch, ts, text, blocks, broadcast)
	return err
}

// sendFiles shares files with the message as their comment: prepared ones
// (uploaded at attach) by id, raw ones through uploadFiles.
func sendFiles(ctx context.Context, api coreAPI, ch, ts, comment string, blocks json.RawMessage, prepared []core.PreparedUpload, raw []core.UploadFile) error {
	if comment != "" {
		comment, _ = transformBlocks(ctx, comment, blocks)
	}
	if len(raw) > 0 {
		if t := currentTaut(); t.ThunkPatched(thUploadFiles) {
			files := make([]taut.File, len(raw))
			for i, f := range raw {
				files[i] = taut.File{Name: f.Name, Data: f.Data}
			}
			ids := make([]string, len(prepared))
			for i, p := range prepared {
				ids[i] = p.ID
			}
			byID := map[string]core.PreparedUpload{}
			for _, p := range prepared {
				byID[p.ID] = p
			}
			_, err := taut.Dispatch(ctx, t, thUploadFiles, uploadFilesArgs{Files: files, ChannelID: ch, ThreadTs: ts, InitialComment: comment, FileIDs: ids},
				func(ctx context.Context, a uploadFilesArgs) (any, error) {
					o := core.UploadOpts{Channel: a.ChannelID, ThreadTS: a.ThreadTs, Comment: a.InitialComment}
					for _, f := range a.Files {
						o.Files = append(o.Files, core.UploadFile{Name: f.Name, Title: f.Name, Data: f.Data})
					}
					for _, id := range a.FileIDs {
						if p, ok := byID[id]; ok {
							o.Prepared = append(o.Prepared, p)
						}
					}
					_, err := api.upload(ctx, o)
					return nil, err
				})
			return err
		}
	}
	_, err := api.upload(ctx, core.UploadOpts{Channel: ch, ThreadTS: ts, Comment: comment, Files: raw, Prepared: prepared})
	return err
}

// ---- uploads at attach ----

// prepareFile uploads one attached file's bytes (addAndUploadPendingFile;
// AnonymizeFilenames renames it). It returns the prepared upload to share
// on send. Call off the UI goroutine.
func prepareFile(ctx context.Context, api coreAPI, name string, data []byte, progress func(sent, total int64)) (core.PreparedUpload, error) {
	if t := currentTaut(); t.ThunkPatched(thPendingFile) {
		var up core.PreparedUpload
		r, err := taut.Dispatch(ctx, t, thPendingFile, pendingFileArgs{File: taut.File{Name: name, Data: data}},
			func(ctx context.Context, a pendingFileArgs) (pendingFileResult, error) {
				n := a.File.Name
				if n == "" {
					n = name
				}
				d := a.File.Data
				if len(d) == 0 {
					d = data
				}
				p, err := api.prepareUpload(ctx, core.UploadFile{Name: n, Title: n, Data: d}, progress)
				if err != nil {
					return pendingFileResult{}, err
				}
				up = p
				return pendingFileResult{FileIDs: []string{p.ID}, Name: p.Name, Title: p.Title, Size: p.Size}, nil
			})
		if err != nil {
			return core.PreparedUpload{}, err
		}
		if up.ID == "" && len(r.FileIDs) > 0 {
			// A plugin answered without calling the original.
			up = core.PreparedUpload{ID: r.FileIDs[0], Name: r.Name, Title: r.Title, Size: r.Size}
		}
		return up, nil
	}
	return api.prepareUpload(ctx, core.UploadFile{Name: name, Title: name, Data: data}, progress)
}

// ---- hooks (composer.addSendCheck / addLinkTransform / patchInlineMarkup) ----

// syncHooks points the composer's plugin hooks at taut while plugins
// register them, and back to nil (the zero-cost native path) when none do.
// It runs on the UI goroutine at the start of every composer Layout (three
// atomic loads), the only goroutine that reads the hook vars. Hooks set by
// someone else (tests, another package) are left alone: syncHooks only
// clears what it installed.
func syncHooks() {
	t := currentTaut()
	on := t != nil && t.HasSendChecks()
	switch {
	case on && SendChecks == nil:
		SendChecks, installed.send = tautSendCheck, true
	case !on && installed.send:
		SendChecks, installed.send = nil, false
	}
	on = t != nil && t.HasLinkTransforms()
	switch {
	case on && LinkTransforms == nil:
		LinkTransforms, installed.link = tautLinkTransform, true
	case !on && installed.link:
		LinkTransforms, installed.link = nil, false
	}
	on = t != nil && t.HasInlineMarkup()
	switch {
	case on && InlineMarkup == nil:
		InlineMarkup, installed.markup = tautInlineMarkup, true
	case !on && installed.markup:
		InlineMarkup, installed.markup = nil, false
	}
}

// installed records which hook vars syncHooks set (UI goroutine only).
var installed struct{ send, link, markup bool }

// linkSource is Taut's addLinkTransform context source for the current
// LinkTransforms call: "paste", or "link-dialog" while the dialog saves.
var linkSource = "paste"

// tautSendCheck runs every composer.addSendCheck off the UI goroutine.
func tautSendCheck(m SendCheck, done func(ok bool)) {
	t := currentTaut()
	if t == nil {
		done(true)
		return
	}
	kind := "channel"
	if m.ThreadTS != "" {
		kind = "thread"
	}
	c := taut.ComposerContext{Kind: kind, ChannelID: m.ChannelID, ThreadTs: m.ThreadTS, Blocks: m.Blocks, Text: m.Text}
	go func() { done(t.SendCheck(context.Background(), c)) }()
}

// tautLinkTransform runs composer.addLinkTransform (ClearURLs) on one URL.
func tautLinkTransform(u string) string {
	ctx, cancel := context.WithTimeout(context.Background(), tautSyncTimeout)
	defer cancel()
	t := currentTaut()
	if t == nil {
		return u
	}
	return t.TransformLink(ctx, u, linkSource)
}

// tautInlineMarkup runs composer.patchInlineMarkup's parse (RealMarkdown).
func tautInlineMarkup(line string) []MarkupSpan {
	ctx, cancel := context.WithTimeout(context.Background(), tautSyncTimeout)
	defer cancel()
	t := currentTaut()
	if t == nil {
		return nil
	}
	return fromTautSpans(t.ParseInlineMarkup(ctx, line))
}

// fromTautSpans converts taut's spans (rune offsets, JSON format) to the
// composer's.
func fromTautSpans(in []taut.MarkupSpan) []MarkupSpan {
	if len(in) == 0 {
		return nil
	}
	out := make([]MarkupSpan, 0, len(in))
	for _, s := range in {
		sp := MarkupSpan{Start: s.Start, End: s.End, ContentStart: s.ContentStart, ContentEnd: s.ContentEnd}
		var f any
		_ = json.Unmarshal(s.Format, &f)
		switch v := f.(type) {
		case string:
			sp.Format = v
		case map[string]any:
			if l, ok := v["link"].(string); ok {
				sp.Format, sp.Link = "link", l
			}
		case nil:
		default:
			continue
		}
		out = append(out, sp)
	}
	return out
}
