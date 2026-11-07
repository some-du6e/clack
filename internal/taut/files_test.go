package taut

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestTextyInputShape: Slick's CharacterCounter finds Slack's editor with
// {filter: isTextyInput}: a react.memo whose type's prototype has a
// getNumCharsOverLimit method. taut's TextyInput native is that shape no
// matter who creates it first, a filter-only patch matches it, and the
// ref its Original gets is Slack's {instance: {quill}}, which the feed
// (the composer's prelude in the app) keeps current.
func TestTextyInputShape(t *testing.T) {
	h := newHarness(t)
	// the native exists before any feed is installed, as when the runtime
	// draws TextyInput before the composer's prelude runs
	var shape struct {
		Memo, Method, Known bool
		Name                string
	}
	h.jsInto(`const c = T.nativeFor('TextyInput')
	return { Memo: c?.$$typeof === Symbol.for('react.memo'), Method: typeof c?.type?.prototype?.getNumCharsOverLimit === 'function',
	  Name: T.componentName(c), Known: typeof c === 'function' }`, &shape)
	if !shape.Memo || !shape.Method || !shape.Known || shape.Name != "TextyInput" {
		t.Fatalf("TextyInput native: %+v", shape)
	}

	// CharacterCounter's patch: filter-only, forwarding a ref to Original
	h.js(`
		const isTexty = (c) => c?.$$typeof === Symbol.for('react.memo') && typeof c.type?.prototype?.getNumCharsOverLimit === 'function'
		T.feedNative('TextyInput', (props) => { const { $test, ...rest } = props; return { state: $test || null, props: rest } })
		globalThis.__texty = { handle: null, changes: 0 }
		T.patchComponent({ filter: isTexty }, (Original) => (props) => {
			const ref = React.useCallback((node) => { globalThis.__texty.handle = node }, [])
			return React.createElement(Original, { ...props, ref })
		})
		return 1`)
	eventually(t, 3*time.Second, "TextyInput patched", func() bool {
		h.ui.mu.Lock()
		defer h.ui.mu.Unlock()
		return has(h.ui.patched, "TextyInput")
	})

	nodes := h.render(1, "TextyInput", map[string]any{"maxLength": 10, "$test": map[string]any{"length": 5, "index": 5, "selected": 0}})
	if n := findNative(nodes, "TextyInput"); n == nil {
		t.Fatalf("no TextyInput native drawn: %v", nodes)
	} else if p, _ := n["p"].(map[string]any); p["$test"] != nil || p["ref"] != nil {
		t.Errorf("the feed's data or the ref reached the native: %v", p)
	}
	var q struct {
		Quill, On                  bool
		Length, Index, Sel, OverBy int
	}
	read := `const hd = globalThis.__texty.handle; const quill = hd?.instance?.quill
		return { Quill: !!quill, On: typeof quill?.on === 'function' && typeof quill?.off === 'function',
		  Length: quill?.getLength(), Index: quill?.getSelection(false)?.index, Sel: quill?.getSelection(false)?.length,
		  OverBy: hd?.instance?.getNumCharsOverLimit?.() }`
	eventually(t, 3*time.Second, "the ref's quill", func() bool {
		h.jsInto(read, &q)
		return q.Quill && q.Length == 6
	})
	if !q.On || q.Index != 5 || q.Sel != 0 || q.OverBy != 0 {
		t.Errorf("quill after 5 characters: %+v", q)
	}

	// typing past the limit and selecting: editor-change fires, the quill follows
	h.js(`const quill = globalThis.__texty.handle.instance.quill
		quill.on('editor-change', () => { globalThis.__texty.changes++ })
		return 1`)
	h.render(1, "TextyInput", map[string]any{"maxLength": 10, "$test": map[string]any{"length": 12, "index": 2, "selected": 4}})
	var changes int
	eventually(t, 3*time.Second, "editor-change after the feed changed", func() bool {
		h.jsInto(`return globalThis.__texty.changes`, &changes)
		return changes > 0
	})
	h.jsInto(read, &q)
	if q.Length != 13 || q.Index != 2 || q.Sel != 4 || q.OverBy != 2 {
		t.Errorf("quill after 12 characters, 4 selected: %+v", q)
	}
}

// fakePending is the composer's side of the pending-file hooks.
type fakePending struct {
	mu        sync.Mutex
	files     []PendingFile
	persisted []string
}

func (f *fakePending) install(t *testing.T) {
	list, rename, persist, edit := PendingFiles, RenamePendingFile, PersistPendingFile, EditFile
	t.Cleanup(func() { PendingFiles, RenamePendingFile, PersistPendingFile, EditFile = list, rename, persist, edit })
	PendingFiles = func() []PendingFile {
		f.mu.Lock()
		defer f.mu.Unlock()
		return append([]PendingFile(nil), f.files...)
	}
	RenamePendingFile = func(id, title string) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := range f.files {
			if f.files[i].ID == id {
				f.files[i].Title = title
				return true
			}
		}
		return false
	}
	PersistPendingFile = func(_ context.Context, id string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, p := range f.files {
			if p.ID == id {
				f.persisted = append(f.persisted, id)
				return nil
			}
		}
		return fmt.Errorf("no pending file %s", id)
	}
	EditFile = func(_ context.Context, fileID, title string) (PendingFile, bool, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for i := range f.files {
			if f.files[i].FileID == fileID {
				f.files[i].Title = title
				return f.files[i], true, nil
			}
		}
		return PendingFile{}, false, nil
	}
}

// TestPendingFileShapes: what AnonymiseFileNames' toggle uses. The
// composer's pending files are state.pendingFileUploads (and state.files
// once uploaded); updatePendingFile is a webpack export with meta.name,
// whose action renames through the composer's hook and is visible in the
// same task; maybePersistPendingFileData and editFileApi are thunks over
// the hooks, and editFileApi's new title is in state.files when it
// resolves.
func TestPendingFileShapes(t *testing.T) {
	f := &fakePending{files: []PendingFile{
		{ID: "clack_pending_1", Name: "x7k2m9q.png", Title: "x7k2m9q.png", Size: 14, Mimetype: "image/png"},
		{ID: "clack_pending_2", Name: "notes.txt", Title: "notes.txt", Size: 3, Mimetype: "text/plain", FileID: "F0UP"},
	}}
	f.install(t)
	h := newHarness(t)

	var got struct {
		Export, Meta            bool
		Keys, FileKeys          []string
		Title, Status, FileName string
		After                   string
		Unknown                 bool
	}
	h.jsInto(`
		const update = T.getExport((v) => typeof v === 'function' && v.meta?.name === 'updatePendingFile')
		const st = () => T.redux.getStore().getState()
		const e = st().pendingFileUploads?.clack_pending_1
		const out = { Export: typeof update === 'function', Meta: update?.meta?.name === 'updatePendingFile',
		  Keys: Object.keys(st().pendingFileUploads), FileKeys: Object.keys(st().files),
		  Title: e?.title, Status: e?.status, FileName: e?.file?.name, Unknown: st().pendingFileUploads.nope === undefined }
		T.redux.getStore().dispatch(update({ pendingFileId: 'clack_pending_1', title: 'vacation.png' }))
		out.After = st().pendingFileUploads?.clack_pending_1?.title
		return out`, &got)
	if !got.Export || !got.Meta {
		t.Fatalf("updatePendingFile export: %+v", got)
	}
	if fmt.Sprint(got.Keys) != "[clack_pending_1 clack_pending_2]" || fmt.Sprint(got.FileKeys) != "[F0UP]" || !got.Unknown {
		t.Errorf("slices: pendingFileUploads %v, files %v, unknown key undefined: %v", got.Keys, got.FileKeys, got.Unknown)
	}
	if got.Title != "x7k2m9q.png" || got.Status != "pending" || got.FileName != "x7k2m9q.png" {
		t.Errorf("pending entry: %+v", got)
	}
	if got.After != "vacation.png" {
		t.Errorf("title after updatePendingFile = %q, want vacation.png in the same task", got.After)
	}
	if fs := PendingFiles(); fs[0].Title != "vacation.png" {
		t.Errorf("the composer's file after updatePendingFile: %+v", fs[0])
	}

	var res struct {
		Persisted, Missing string
		Edited, Other      string
	}
	h.jsInto(`
		const out = {}
		await T.redux.dispatchThunk('maybePersistPendingFileData', { pendingFileId: 'clack_pending_1' })
		out.Persisted = 'ok'
		try { await T.redux.dispatchThunk('maybePersistPendingFileData', { pendingFileId: 'gone' }); out.Missing = 'resolved' } catch (e) { out.Missing = 'rejected' }
		await T.redux.dispatchThunk('editFileApi', { fileId: 'F0UP', title: 'shopping.txt' })
		out.Edited = T.redux.getRawState()?.files?.F0UP?.title
		try { await T.redux.dispatchThunk('editFileApi', { fileId: 'F0ELSE', title: 'x' }); out.Other = 'resolved' } catch (e) { out.Other = 'rejected' }
		return out`, &res)
	if res.Persisted != "ok" || res.Missing != "rejected" {
		t.Errorf("maybePersistPendingFileData: %+v", res)
	}
	f.mu.Lock()
	persisted := fmt.Sprint(f.persisted)
	f.mu.Unlock()
	if persisted != "[clack_pending_1]" {
		t.Errorf("persisted %s, want [clack_pending_1]", persisted)
	}
	if res.Edited != "shopping.txt" || res.Other != "rejected" {
		t.Errorf("editFileApi: %+v (want files.F0UP.title shopping.txt when it resolves, unknown ids rejected)", res)
	}
}
