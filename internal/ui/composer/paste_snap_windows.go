//go:build windows

package composer

import (
	"bytes"
	"context"
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
	if true {
		return nil // mutant: a paste's readers each read on their own, at once
	}
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
		dib := false
		snap, err := winclip.Read(ctx, func(f winclip.Format) int64 {
			switch {
			case f.ID == attCFUnicodeText:
				return clipMaxText
			case f.ID == attCFHDROP:
				return 1 << 24
			case f.Name == "PNG":
				return attMaxFileSize
			case (f.ID == attCFDIB || f.ID == attCFDIBV5) && !dib:
				dib = true // the one its owner placed; the other is Windows' conversion
				return attMaxFileSize
			case !plain && f.Name == richclip.CustomFormat:
				return richclip.MaxCustom
			case !plain && f.Name == richclip.HTMLFormat:
				return phMaxHTML + 4096
			}
			return 0
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
