package ui

// Scrolling a message's video tile into view, with no player in it: the
// App tests' and the real-window test's (round 3's review, a NIT: a red
// branch replaces only the hooks files, and this needs none).

import "image"

// videoScrollStep is a step bringing message ts's video tile wholly into
// list l, as the last frame laid it out: the list may be anywhere (the
// conversation opened at its end, a jump's message 64dp under its top),
// so in a short window #files-gallery's tall rows can leave the tile
// under the list (FileRects aren't clipped to it) or its row not on
// screen at all. It scrolls toward the tile, or
// toward its row by the rows on screen, and reports where the tile (empty
// while its row is off the screen) and the list were, in true once the
// tile was wholly in the list.
func videoScrollStep(l channelList, ts string) (tile, list image.Rectangle, in bool) {
	list = l.ListRect()
	for _, r := range l.FileRects() {
		if r.TS == ts && r.File.IsVideo {
			tile = r.R
		}
	}
	switch {
	case list.Empty():
	case !tile.Empty() && tile.In(list):
		return tile, list, true
	case !tile.Empty() && tile.Min.Y < list.Min.Y:
		l.ScrollPx(tile.Min.Y - list.Min.Y - 16)
	case !tile.Empty():
		l.ScrollPx(tile.Max.Y - list.Max.Y + 16)
	default:
		rows := l.RowRects()
		switch {
		case len(rows) == 0:
		case tsBefore(ts, rows[0].TS):
			l.ScrollPx(-list.Dy() / 2)
		case tsBefore(rows[len(rows)-1].TS, ts):
			l.ScrollPx(list.Dy() / 2)
		}
	}
	return tile, list, false
}
