package fakeslack

// #gif-wall (Size.GIFs): a channel where every message is a large
// animated GIF, as Hack Club's are (synthetic: generated frames). Even
// messages share a GIF file as Slack sends one (thumb_360_gif and
// thumb_480_gif to play, deanimate and deanimate_gif for its still), odd
// ones are a /giphy attachment (image_url, image_width, image_height).
// The memory test plays it (internal/ui TestPerfGIFMemory: perf.yml).
//
// The images are served from /img/: <w>x<h>/gw<seed>.gif is an animated
// GIF of GIFFrames frames 40 ms apart, looping (one of gifVariants bodies
// per size, so hundreds of URLs don't each cost an encode), and
// <w>x<h>/gw<seed>_deanimate.png its first frame. Only "gw" seeds: the
// generated custom emoji's .gif URLs stay the gradient PNGs every other
// fixture was drawn with.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	// GIFWallID is #gif-wall's id (sorted after every C0FAKE channel, so
	// the drivers' "first channels" don't move).
	GIFWallID = "C0GIFWALL01"
	// GIFFrames is how many frames a generated GIF has.
	GIFFrames   = 24
	gifVariants = 6
)

var gifBodies sync.Map // "wxh/v" -> []byte

// gifBody is variant v of a w×h animated GIF: every frame a full frame
// (no reuse of the last one), a diagonal pattern moving 3 colours a
// frame, so it compresses about as a Giphy rendition does (~1 MB at
// 480×270).
func gifBody(w, h, v int) []byte {
	k := fmt.Sprintf("%dx%d/%d", w, h, v)
	if b, ok := gifBodies.Load(k); ok {
		return b.([]byte)
	}
	pal := color.Palette(palette.WebSafe)
	g := &gif.GIF{Config: image.Config{Width: w, Height: h, ColorModel: pal}}
	for f := 0; f < GIFFrames; f++ {
		m := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := 0; y < h; y++ {
			row := m.Pix[y*m.Stride : y*m.Stride+w]
			for x := range row {
				row[x] = uint8((x/3 + y/3 + 3*f + 37*v + (x/11^y/7)%9) % len(pal))
			}
		}
		g.Image = append(g.Image, m)
		g.Delay = append(g.Delay, 4)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var b bytes.Buffer
	_ = gif.EncodeAll(&b, g)
	gifBodies.Store(k, b.Bytes())
	return b.Bytes()
}

// gifStill is gifBody(w, h, v)'s first frame as a PNG (deanimate).
func gifStill(w, h, v int) []byte {
	g, err := gif.DecodeAll(bytes.NewReader(gifBody(w, h, v)))
	if err != nil || len(g.Image) == 0 {
		return nil
	}
	var b bytes.Buffer
	_ = png.Encode(&b, g.Image[0])
	return b.Bytes()
}

// gifImage serves /img/ paths ending in .gif or _deanimate.png.
func gifImage(path string) ([]byte, string, bool) {
	dims, seed, ok := strings.Cut(path, "/")
	var w, h int
	if !ok || !strings.HasPrefix(seed, "gw") || strings.Contains(seed, "/") ||
		!(strings.HasSuffix(seed, ".gif") || strings.HasSuffix(seed, "_deanimate.png")) {
		return nil, "", false
	}
	if n, _ := fmt.Sscanf(dims, "%dx%d", &w, &h); n != 2 || w <= 0 || h <= 0 || w > 2048 || h > 2048 {
		return nil, "", false
	}
	v := 0
	for _, c := range strings.TrimSuffix(strings.TrimSuffix(seed, ".gif"), "_deanimate.png") {
		v = (v*31 + int(c)) % gifVariants
	}
	if strings.HasSuffix(seed, ".gif") {
		return gifBody(w, h, v), "image/gif", true
	}
	return gifStill(w, h, v), "image/png", true
}

// addGIFWall adds #gif-wall with Size.GIFs messages.
func (s *Server) addGIFWall() {
	s.convs = append(s.convs, slack.Conversation{ID: GIFWallID, Name: "gif-wall", IsChannel: true, IsMember: true,
		Created: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Unix(), NumMembers: 900, ContextTeamID: TeamID,
		Purpose: &slack.Purpose{Value: "GIFs. Synthetic."}})
	base := time.Date(2026, 11, 7, 6, 0, 0, 0, time.UTC).Unix()
	img := func(w, h int, seed string) string { return fmt.Sprintf("%simg/%dx%d/%s", s.URL, w, h, seed) }
	msgs := make([]slack.Message, 0, s.Size.GIFs)
	for i := s.Size.GIFs - 1; i >= 0; i-- { // newest first
		ts := fmt.Sprintf("%d.%06d", base+int64(i)*90, 100)
		u := s.users[(i*7+3)%len(s.users)].ID
		m := slack.Message{Type: "message", TS: ts, User: u}
		if i%2 == 0 {
			id := fmt.Sprintf("gwf%04d", i)
			m.Files = []slack.File{{ID: fmt.Sprintf("F0GIF%05d", i), Name: id + ".gif", Title: id + ".gif", Mimetype: "image/gif",
				Filetype: "gif", PrettyType: "GIF", Size: int64(len(gifBody(480, 270, i%gifVariants))), Mode: "hosted",
				Created: base + int64(i)*90, User: u,
				URLPrivate: img(498, 280, id+".gif"), URLPrivateDownload: img(498, 280, id+".gif"),
				Thumb360: img(360, 203, id+"_360.png"), Thumb360W: 360, Thumb360H: 203,
				Thumb480: img(480, 270, id+"_480.png"), Thumb480W: 480, Thumb480H: 270,
				Thumb360GIF: img(360, 203, id+"_360.gif"), Thumb480GIF: img(480, 270, id+"_480.gif"),
				Deanimate: img(498, 280, id+"_deanimate.png"), DeanimateGIF: img(498, 280, id+"_deanimate.png"),
				OriginalW: 498, OriginalH: 280}}
		} else {
			id := fmt.Sprintf("gwa%04d", i)
			m.Text = "/giphy " + s.words[i%len(s.words)]
			m.Attachments = []slack.Attachment{{ID: 1, Fallback: "giphy: " + id, Title: s.words[(i*3)%len(s.words)],
				ImageURL: img(480, 270, id+".gif"), ImageWidth: 480, ImageHeight: 270, Footer: "Posted using /giphy"}}
		}
		msgs = append(msgs, m)
	}
	s.hist[GIFWallID] = msgs
}
