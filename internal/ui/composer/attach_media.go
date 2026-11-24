package composer

import (
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The gallery's media tiles: Slack's MultiFileUpload draws each file with
// maybeRenderImageFile (CDN build bv1-13, boot-shared tTyz), in this
// order:
//
//   - a File being uploaded that is an image it can show: PendingImageFile;
//     an image (a dataUrl, or fileIsImage of a file object): DraftImageFile
//     (drawThumb);
//   - a File (this session's upload) of a video/* type
//     (isPendingVideoFile), or a video clip (7OiV): PendingVideoFile
//     (drawPendingVideo);
//   - a file object that is a playable video (fileIsPlayableVideo):
//     DraftVideoFile (drawDraftVideo);
//   - anything else is PendingFile, whose renderContent is FileAudio's
//     AudioPlayer for a playable audio file object (fileIsPlayableAudio:
//     drawAudioCard), a recorded clip's PendingAudioMessageUpload
//     (drawClipCard), else a MediumFileEntity card (drawCard).
//
// A file restored with a draft is the store's file object
// (draftComposerFiles), so a restored video or audio file draws as media;
// a file uploaded this session stays the browser's File, so an mp3 is a
// card and a video the pending tile even once it's up.

// pendingVideoLocked: c is this session's File of a video type
// (isPendingVideoFile: Chromium's File.type; a video clip's subtype, 7OiV,
// needs a video recorder, which Clack doesn't have) (a.mu held).
func (c *attChip) pendingVideoLocked() bool {
	return c.data != nil && c.clip == nil && strings.HasPrefix(c.ftype, "video/")
}

// draftVideoLocked: c is a restored playable video (a.mu held).
func (c *attChip) draftVideoLocked() bool {
	return c.data == nil && c.server != nil && c.server.IsVideo
}

// restoredAudioLocked: c is a restored playable audio file (a.mu held).
func (c *attChip) restoredAudioLocked() bool {
	return c.data == nil && c.server != nil && c.server.IsAudio
}

// drawPendingVideo is PendingVideoFile: a 62px tile, radius 12, of the
// video's first frame (preloadPreviewData's get_video_thumbnail_data, the
// frame at 0.1 s, at most 960px, cover), or while there's none
// p-pending_video_file__video_thumb_placeholder's black, under the white
// play-filled (Icon, 20px) and the tile's 1px rgba(sk_foreground_low,
// .13) ring (its :after). Clack has no frame for it yet: the media
// backends draw a frame only into a window's GPU frame
// (media.FrameCopier reads that one back), so the tile is the
// placeholder Slack draws until its frame is ready, or when Chromium
// can't decode the file.
func (a *attachments) drawPendingVideo(gtx layout.Context, th *theme.Theme, v attView, r image.Rectangle, rad int) {
	fillRRect(gtx, r, rad, color.NRGBA{A: 0xff})
	drawPlayGlyph(gtx, r)
	strokeRRect(gtx, r, rad, max(1, gtx.Dp(1)), th.C.Border)
}

// drawDraftVideo is DraftVideoFile: MediaFileThumbnail (CVlh) in a 62px
// tile, radius 12 (p-draft_video_file): the poster (thumb_video_ts ??
// thumb_video, the file object's Thumb) as its background, cover, under
// the 20% black overlay and the white play-filled; no ring
// (p-draft_video_file has no :after), nothing under a video with no
// poster but the overlay.
func (a *attachments) drawDraftVideo(gtx layout.Context, th *theme.Theme, v attView, r image.Rectangle, rad int) {
	if v.thumbPx > 0 {
		paintThumb(gtx, v, r, rad)
	}
	fillRRect(gtx, r, rad, color.NRGBA{A: 0x33}) // p-media_file_thumbnail__overlay
	drawPlayGlyph(gtx, r)
}

// drawAudioCard is PendingFile's renderContent for a restored playable
// audio file: FileAudio's AudioPlayer (p-pending_file__audio_player,
// 184px, mini) in the pillow (1px border, radius 12, 12 in), as an
// uploaded clip's is; where there's no player, the mini player's look
// with its play button disabled.
func (a *attachments) drawAudioCard(gtx layout.Context, th *theme.Theme, v attView, r image.Rectangle, rad int) {
	fillRRect(gtx, r, rad, th.C.Bg)
	strokeRRect(gtx, r, rad, max(1, gtx.Dp(1)), th.C.Border)
	f := v.server
	if a.layoutMiniPlayer(gtx, f, r) {
		return
	}
	var dur string
	if f.DurationMS > 0 {
		dur = audioTime(math.Floor(float64(f.DurationMS) / 1000))
	}
	samples := make([]float64, len(f.Waveform))
	for i, s := range f.Waveform {
		samples[i] = float64(s) / 100
	}
	drawIdlePlayer(gtx, th, r, dur, samples)
}

// paintThumb paints v's thumb, made square (cover) at v.thumbPx, over r
// clipped to radius rad.
func paintThumb(gtx layout.Context, v attView, r image.Rectangle, rad int) {
	defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	sc := float32(r.Dx()) / float32(v.thumbPx)
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sc, sc))).Push(gtx.Ops).Pop()
	v.thumb.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// drawPlayGlyph is the media tiles' white play-filled (20px), centred
// in r.
func drawPlayGlyph(gtx layout.Context, r image.Rectangle) {
	ic := gtx.Dp(20)
	offset(gtx, r.Min.X+(r.Dx()-ic)/2, r.Min.Y+(r.Dy()-ic)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("play-filled")(gtx, unit.Dp(20), rgb(0xffffff))
	})
}
