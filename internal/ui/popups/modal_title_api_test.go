package popups

import "image"

// The title wrap tests' window on the modal (modal_title_test.go); a red
// run on the old code swaps this file for one written against its API.

func mtLines(m *Modal) []string         { return m.TitleLines() }
func mtClose(m *Modal) image.Rectangle  { return m.CloseRect() }
func mtTruncated(o ModalOpts) ModalOpts { o.TruncateTitle = true; return o }
