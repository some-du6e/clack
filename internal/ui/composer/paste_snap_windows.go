//go:build windows

package composer

import (
	"bytes"
	"context"
	"errors"
	"time"

	"gioui.org/app/winclip"

	"github.com/some-du6e/clack/internal/ui/richclip"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

const attCFUnicodeText = 13

// clipMaxText caps a paste's text copy (256 MB; the paste limit refuses
// far less: EvPasteLimit).
const clipMaxText = 256 << 20

// startPasteSnap reads the clipboard for one paste in one session: the
// text, unless plain the rich text (web custom data, then "HTML Format"),
// the files (CF_HDROP) and the picture ("PNG", else the DIB its owner
// put there). Each copy is bounded by its block and its limit
// (winclip.CopyData) and parsed after the session closes.
func startPasteSnap(plain bool) *pasteSnap {
	s := &pasteSnap{done: make(chan struct{}), imgErr: errAttNoClipImage}
	if !plain {
		s.rich = &clipRead{done: make(chan struct{})}
	}
	go func() {
		defer close(s.done)
		if s.rich != nil {
			defer close(s.rich.done)
		}
		uiguard.OffUI("a clipboard session")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		snap, err := winclip.ReadPlan(ctx, func(fs []winclip.Format) map[uint32]int64 {
			m := imagePlan(fs, false)
			for _, f := range fs {
				switch {
				case f.ID == attCFUnicodeText:
					m[f.ID] = clipMaxText
				case f.ID == attCFHDROP:
					m[f.ID] = 1 << 24
				case !plain && f.Name == richclip.CustomFormat:
					m[f.ID] = richclip.MaxCustom
				case !plain && f.Name == richclip.HTMLFormat:
					m[f.ID] = phMaxHTML + 4096
				}
			}
			return m
		})
		if err != nil {
			s.imgErr = errAttClipBusy
			return
		}
		if b, ok := snap.Bytes(attCFUnicodeText); ok {
			s.text = winclip.UTF16Text(b)
		}
		if s.rich != nil {
			s.rich.html, s.rich.texty = snapRich(snap)
		}
		if b, ok := snap.Bytes(attCFHDROP); ok {
			s.files = attParseHDROP(b)
		}
		if len(s.files) == 0 {
			s.img, s.imgMime, s.imgErr = snapImage(snap, false)
			if s.imgErr == errSnapNeedDIB {
				// the "PNG" wasn't one: the DIB, as base fell back to it
				s.img, s.imgMime, s.imgErr = attClipboardDIB(ctx)
			}
		}
	}()
	return s
}

// snapRich is the paste's rich text, as startClipHTML reads it: Slack's
// own copy (slack/texty, then slack/html), else the HTML.
func snapRich(snap *winclip.Snapshot) (string, *Doc) {
	if m := richclip.CustomFrom(snap); m != nil {
		if d := textyDoc(m["slack/texty"]); d != nil {
			return "", d
		}
		if h := m["slack/html"]; h != "" && len(h) <= phMaxHTML {
			return h, nil
		}
	}
	if f, ok := snap.Named(richclip.HTMLFormat); ok {
		if b, ok := snap.Bytes(f.ID); ok {
			if h := cfHTML(b); len(h) > 0 && len(h) <= phMaxHTML {
				return clipHTMLText(h), nil
			}
		}
	}
	return "", nil
}

// imagePlan is the picture formats a read copies: none for an office
// app's copy (rich text and a rendered picture of it) unless explicit, as
// base decided from the format list before reading any picture; the
// "PNG" alone when there is one (it keeps transparency); else the DIB its
// owner placed (the other one is Windows' conversion).
func imagePlan(fs []winclip.Format, explicit bool) map[uint32]int64 {
	m := map[uint32]int64{}
	var names []string
	var png, dib uint32
	for _, f := range fs {
		if f.Name != "" {
			names = append(names, f.Name)
		}
		switch {
		case f.Name == "PNG" && png == 0:
			png = f.ID
		case (f.ID == attCFDIB || f.ID == attCFDIBV5) && dib == 0:
			dib = f.ID
		}
	}
	if !explicit && attRichCopy(names) {
		return m
	}
	switch {
	case png != 0:
		m[png] = attMaxFileSize
	case dib != 0:
		m[dib] = attMaxFileSize
	}
	return m
}

// errSnapNeedDIB: the snapshot's "PNG" wasn't a PNG; the DIB is read on
// its own (attClipboardDIB).
var errSnapNeedDIB = errors.New("the clipboard's PNG isn't one")

// snapImage is the clipboard's picture as a PNG: unless explicit none for
// an office app's copy (rich text and a rendered picture of it), the
// "PNG" when it is one, else the DIB its owner placed, converted after the
// session closed (other apps aren't kept waiting).
func snapImage(snap *winclip.Snapshot, explicit bool) ([]byte, string, error) {
	var names []string
	var png, dib uint32
	for _, f := range snap.Formats {
		if f.Name != "" {
			names = append(names, f.Name)
		}
		switch {
		case f.Name == "PNG" && png == 0:
			png = f.ID
		case (f.ID == attCFDIB || f.ID == attCFDIBV5) && dib == 0:
			dib = f.ID
		}
	}
	if !explicit && attRichCopy(names) {
		return nil, "", errAttNoClipImage
	}
	if png != 0 {
		if snap.TooBig[png] {
			return nil, "", errAttTooBig
		}
		if b, ok := snap.Bytes(png); ok && bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) {
			return b, "image/png", nil
		}
		if dib != 0 {
			return nil, "", errSnapNeedDIB
		}
	}
	if dib == 0 {
		return nil, "", errAttNoClipImage
	}
	if snap.TooBig[dib] {
		return nil, "", errAttTooBig
	}
	b, ok := snap.Bytes(dib)
	if !ok {
		return nil, "", errAttNoClipImage
	}
	p, err := attDIBToPNG(b)
	if err != nil {
		return nil, "", err
	}
	if len(p) > attMaxFileSize {
		return nil, "", errAttTooBig
	}
	return p, "image/png", nil
}
