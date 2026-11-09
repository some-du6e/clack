# Clack's patches to Gio v0.10.3

This directory is `gioui.org@v0.10.3` from the Go module cache (byte for
byte, LICENSE included) plus the changes below. The root `go.mod` points at
it with `replace gioui.org => ./third_party/gioui.org`. When an upstream
release carries the fix, drop the replace and delete this directory.

To see the whole diff against the release:

    go mod download gioui.org@v0.10.3
    diff -ru "$(go env GOMODCACHE)/gioui.org@v0.10.3" third_party/gioui.org

It should show only `gpu/gpu.go` and these two files (`CLACK_PATCHES.md`,
`UPSTREAM.patch`).

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
