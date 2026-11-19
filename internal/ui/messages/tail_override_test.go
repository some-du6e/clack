package messages

// MessageText's tree hands its parts the msg its plugin made (BetterBlock
// drops a blocked member's file and attachment there); MessageFiles and
// MessageKitAttachments draw the files and attachments of the msg they
// get. Patched to pass it on, their trees hand back MessageText's msg,
// which read as one the row had sent (its fingerprint is in bodySent),
// and the cards the plugin dropped stayed drawn. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	tailChan     = "C0TAILOVR1"
	tailDropFile = "F0TAILDROP"
	tailKeepFile = "F0TAILKEEP"
)

// tailDropJS drops one file and every attachment in MessageText's msg,
// and passes MessageFiles' and MessageKitAttachments' msg on.
var tailDropJS = pluginJS(`
class DropTail extends TautPlugin {
  static id = 'DropTail'
  static pluginName = 'Drop a file (test)'
  static description = 'test: MessageText drops a file and the attachments; the file and attachment parts pass their msg on'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('MessageText', (Original) => (props) => {
      const m = props.msg
      const msg = React.useMemo(() => (m ? { ...m, files: (m.files || []).filter((f) => f.id !== '` + tailDropFile + `'), attachments: [] } : m), [m])
      return React.createElement(Original, { ...props, msg })
    })
    const pass = (Original) => (props) => React.createElement(Original, { ...props, msg: props.msg })
    this.api.patchComponent('MessageFiles', pass)
    this.api.patchComponent('MessageKitAttachments', pass)
  }
}
return DropTail`)

func TestMessageTextDropsFilesAndAttachments(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: tailChan, Name: "plans", IsChannel: true, IsMember: true, NumMembers: 12})
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	file := func(id, name, user string) slack.File {
		return slack.File{ID: id, Name: name, Title: name, Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Mode: "hosted", User: user}
	}
	app.S.SetLatest(tailChan, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "see these", Blocks: shotRich(shotSection("see these")),
		Files:       []slack.File{file(tailDropFile, "plan.pdf", "UBEN00001"), file(tailKeepFile, "notes.pdf", "UANA00001")},
		Attachments: []slack.Attachment{{ID: 1, Fallback: "a link", Title: "A link", TitleLink: "https://example.com/a", Text: "its preview"}}}}, false)
	patchedEnv(t, app, tailDropJS, "MessageText", "MessageFiles", "MessageKitAttachments")
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tailChan)
	v.Preview = true
	frame := frameFn(v)
	// drawn are the file ids and attachments the row's body draws
	drawn := func() (files []string, atts int) {
		r := v.s.rows[ts]
		if r == nil || r.rv == nil {
			return nil, 0
		}
		for _, b := range r.rv.Doc().Blocks {
			switch {
			case b.Kind == richtext.FileBlock && b.File != nil:
				files = append(files, b.File.ID)
			case b.Kind == richtext.AttachmentBlock && b.Att != nil:
				atts++
			}
		}
		return files, atts
	}
	until(t, 10*time.Second, "MessageText's tree taken", frame, func() bool {
		r := v.s.rows[ts]
		return r != nil && r.textOv != nil && itemHeight(v.s, ts) > 0
	})
	settled := false
	var bad []string
	for i := 0; i < 80; i++ {
		frame()
		files, atts := drawn()
		ok := len(files) == 1 && files[0] == tailKeepFile && atts == 0
		if ok {
			settled = true
		} else if settled && len(bad) < 6 {
			bad = append(bad, fmt.Sprintf("frame %d: the body draws files %v and %d attachments again; MessageText's plugin dropped %s and the attachment", i, files, atts, tailDropFile))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !settled {
		files, atts := drawn()
		t.Errorf("the body draws files %v and %d attachments; MessageText's plugin dropped %s and the attachment (its parts passed the msg on)", files, atts, tailDropFile)
	}
	for _, b := range bad {
		t.Error(b)
	}
}
