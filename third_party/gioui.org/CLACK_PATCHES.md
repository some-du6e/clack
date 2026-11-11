# Clack's patches to Gio v0.10.3

This directory is `gioui.org@v0.10.3` from the Go module cache (byte for
byte, LICENSE included) plus the changes below. The root `go.mod` points at
it with `replace gioui.org => ./third_party/gioui.org`. When an upstream
release carries the fix, drop the replace and delete this directory.

To see the whole diff against the release:

    go mod download gioui.org@v0.10.3
    diff -ru "$(go env GOMODCACHE)/gioui.org@v0.10.3" third_party/gioui.org

It should show only `gpu/gpu.go`, `widget/index.go`, `widget/editor.go`,
`widget/text.go`, `widget/clack_spans.go`, `op/op.go`,
`internal/ops/ops.go`, `app/os_x11.go`, `app/clack_gpu.go` and these two
files (`CLACK_PATCHES.md`, `UPSTREAM.patch`).

## 1. gpu: drop opacity layers covered by a full-window opaque paint

`gpu/gpu.go`, `drawOps.collect` and `renderer.drawLayers`.

When a paint is a uniform opaque colour over the whole viewport and no
opacity layer is open, `collect` throws away the image ops recorded so far
(`d.imageOps = d.imageOps[:0]`) and turns the paint into the clear colour.
It kept `d.layers`, though. Opacity layers pushed and popped earlier in the
frame still held `opStart`/`opEnd` indices into the longer, discarded
`imageOps`, and `drawLayers` then indexed past the end:

    panic: runtime error: index out of range [609] with length 135
        panic: active render pass   (Metal)
    gioui.org/gpu.(*renderer).drawLayers  gpu.go:872

Karim hit it in Clack alpha.18 on macOS. In Clack the trigger is any
`paint.PushOpacity` (a dimmed pending message, a fade) followed later in
the same frame by a full-window opaque fill.

Fix: also truncate `d.layers` when the image ops are scrapped. The opacity
stack is empty there, so every recorded layer has already ended and is
fully covered by the opaque paint. As a second guard, `drawLayers` skips any
layer whose op range is empty or out of bounds instead of panicking with a
render pass open.

Tests: `internal/ui/theme/pixeltest` (`TestOpaqueFillAfterOpacityLayer`,
raw Gio ops) and `internal/ui/richview` (`TestDimmedViewUnderOpaqueFill`).
CI runs both against this copy (they must pass) and against stock
v0.10.3 (they must panic).

`UPSTREAM.patch` is the same change as a `git am` patch for
git.sr.ht/~eliasnaur/gio, made on upstream main 2919cda (whose
`gpu/gpu.go` has the same bug). It adds a `gpu/internal/rendertest` case and
its reference image; that test panics on main without the fix and
passes with it. It has not been sent. Gio wants a `Signed-off-by` line
(DCO), so whoever sends it adds their own (`git am -s`).

## 2. widget: caret positions inside multi-glyph clusters

`widget/index.go`, `glyphIndex.Glyph`.

When a cluster ends, the index spreads its runes' caret positions over the
cluster's width, starting from the X of the glyph that ends the cluster.
For a one-glyph cluster that is the cluster's start. For a cluster of
several glyphs it isn't: a base plus a zero-width mark (an emoji and its
skin tone modifier or VS16, combining accents) ends with the mark, which
sits at the cluster's right edge, so every position in the cluster, and
`Editor.Regions` for it, came out one cluster-width too far right. In
Clack the emoji images drawn over `widget.Editor` text (search box,
`plain_text_input`, theme.Editor) landed on the text after 👋🏾.

Fix: remember the X of the cluster's first glyph and spread the positions
from there (left-to-right runs; right-to-left keeps upstream's arithmetic).

Test: `internal/ui/theme` `TestThemeEditorEmojiRegions`. Not yet in
`UPSTREAM.patch`.

## 3. op: Checkpoint and Rollback

`op/op.go` (`Ops.Checkpoint`, `Ops.Rollback`) and `internal/ops/ops.go`
(`SaveCheckpoint`, `Rollback`). An addition, not a fix: no upstream API
changes behaviour.

A widget that panics half way through its layout leaves the Ops with
transforms and clips pushed and macros still recording. Recovering from
the panic isn't enough: the caller's own deferred `Pop`s then panic with
"stack push and pop must not cross macro boundary", because the macro
stack still has the abandoned macro on top. Clack alpha.24 crashed on
macOS this way when a canvas's block list changed during its layout
(internal/ui/canvasview, `layoutDoc`).

`Rollback` truncates the op data and references to the checkpoint and
restores the macro and op stacks' current ids (their counters keep going,
so a stale handle never matches). canvasview's `View.Layout` takes a
checkpoint, recovers a panic, rolls back and draws the canvas error state
in place.

Test: `internal/ui/canvasview` `TestLayoutPanicRecovered` (a panic with
pushed transforms, clips and an open macro inside a recording parent; the
parent then pops and stops cleanly).

## 4. app: IgnoreGPUBlocklist

`app/os_x11.go` (`x11Window.NewContext`) and `app/clack_gpu.go`. An
addition: nothing changes unless Clack calls it.

Gio never uses Vulkan on X11 (`vulkanBuggy`: it doesn't work reliably on
NVIDIA systems, gioui.org/issue/347). That is Gio's one GPU blocklist, the
counterpart of Chromium's: Slick's Snappy plugin appends Chromium's
`ignore-gpu-blocklist` switch when "Ignore GPU blocklist" is on, and Clack
maps the switch to `app.IgnoreGPUBlocklist()` at the next start
(internal/slick/switches.go, cmd/clack). An X11 window then tries Vulkan
first and falls back to OpenGL ES, as before, when Vulkan fails. Wayland,
macOS and Windows have no blocklist; it changes nothing there.

## 5. widget: Mod+Backspace and Mod+Delete in Editor

`widget/editor.go`, `Editor.processKey`'s delete filters and
`Editor.command` (plus `deleteToLineEdge`).

`command` checks `ModShortcut` before the delete keys. Outside macOS
`ModShortcut` and `ModShortcutAlt` are both Ctrl, so Ctrl+Backspace and
Ctrl+Delete landed in the shortcut branch, which has no case for them,
and the word deletion meant for them never ran. On macOS the delete
filters didn't list `ModShortcut` (Cmd), so Cmd+Backspace never reached
the editor and AppKit beeped. Karim hit it in a Block Kit modal's
`plain_text_input` on 2026-11-10 ("u cant delete the modal editable
field?"); every `widget.Editor` in Clack had it (search fields, dialogs,
Block Kit inputs).

Fix: with `ModShortcutAlt` held (Ctrl elsewhere, Option on macOS) the
delete keys remove a word; with Cmd alone on macOS they delete to the
start or end of the line, or the selection, and at the line's edge one
grapheme cluster, which joins the lines. That is what browser text fields
do, so Slack's inputs behave the same.

Test: `internal/ui/blockkit` `TestModalTextInputsEdit` (every Block Kit
text input in a modal; ci runs it on Linux and macOS). Not in
`UPSTREAM.patch`.

## 6. widget: styled spans in an Editor's text

`widget/clack_spans.go` (new: `TextSpan`, `Editor.SetSpans`,
`Editor.Spans`, `textView.layoutSpans`) and `widget/text.go` (the
`textView.spans` field and a hook at the top of `layoutText`). An
addition: an Editor without spans shapes its text as before.

Slack's search box draws its filter tokens as pills inside the text:
`from:@Karim` with 23px of padding before it for the modifier's icon,
4px after it, and the value after the first `:` in bold
(`.c-member_slug--search-filter`, docs/SEARCH-AUDIT.md). A
`widget.Editor` shapes its whole text in one font with no room for
either, so the caret, selections and clicks would not line up with
pills drawn on top.

`SetSpans` takes rune ranges, each with an optional font and empty space
(pixels) before and after it. For a single-line, unmasked editor whose
text has no line break, `layoutText` then shapes the text in segments
split at the spans' edges, each in its span's font, and moves each
segment's glyphs right by the width of everything before it (earlier
segments plus the spans' space). Only the last glyph keeps
`FlagLineBreak` and only the first `FlagParagraphStart`, so the index
sees one line; the text's bounds include the space at either end. The
glyph index builds the caret positions from those glyphs, so `Regions`,
the caret, selection painting and pointer hits all follow the styled
layout. Spans are ignored (the text is shaped as usual) for multi-line
or masked editors.

Test: `internal/ui/popups` `TestSearchTokenPills` (a filter token's
modifier starts 24px after the text before it, its value is as wide as
the bold measurement, the text after it starts 4px after the value).
Not in `UPSTREAM.patch`.
