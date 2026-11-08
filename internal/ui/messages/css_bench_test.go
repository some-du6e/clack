package messages

import (
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// mlDeletedCSS is the rule Slick MessageLogger installs for its default
// (red) deleted style: the wrapper it puts around a deleted row, and every
// descendant.
const mlDeletedCSS = `.slick-ml-deleted, .slick-ml-deleted * { color: #e01e5a !important; }`

// rowWrap is a stand-in for MessageLogger's <div className="slick-ml-deleted">
// around rows: the views laid out inside it are its descendants.
var rowWrap = slackc.Class("")

// wrapped lays v out inside an element with class cls ("" = none).
type wrapped struct {
	v interface {
		Layout(layout.Context) layout.Dimensions
	}
	cls string
}

func (w wrapped) Layout(gtx layout.Context) layout.Dimensions {
	st := rowWrap.EnterClass(gtx, 0, 0, w.cls)
	defer st.Leave()
	return w.v.Layout(gtx)
}

// BenchmarkListFrameCSS is BenchmarkListFrame with plugin CSS installed:
// MessageLogger's deleted rule matching nothing (idle), and matching every
// row and all its parts.
func BenchmarkListFrameCSS(b *testing.B) {
	for _, mode := range []string{"ml-idle", "ml-every-row"} {
		b.Run(mode, func(b *testing.B) {
			SetTaut(nil)
			slackc.SetStyle("bench:ml", mlDeletedCSS)
			defer slackc.RemoveStyle("bench:ml")
			app, _ := newTestApp(b)
			v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tGen)
			v.Preview = true
			cls := ""
			if mode == "ml-every-row" {
				cls = "slick-ml-deleted"
			}
			f := frameFn(wrapped{v: v, cls: cls})
			loadList(v, f)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				f()
			}
		})
	}
}

// loadList runs frames until v's snapshot is loaded, then a few more.
func loadList(v *ChannelView, f func()) {
	for i := 0; i < 200 && !(v.s.snap.loaded && len(v.s.items) > 0); i++ {
		f()
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		f()
	}
}
