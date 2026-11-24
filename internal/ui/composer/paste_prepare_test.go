package composer

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Every paste goes through Slack's clipboard's preparePastedDelta when a
// plugin patched it (PreparePaste) with the caret's formats, and what it
// returns is what goes in: rich text, a plain paste, and the composer's
// own copy. Into code its formats say so. A hook that leaves the paste
// (nil) leaves it as it was.
func TestPastePreparedByPlugin(t *testing.T) {
	const dirty, clean = "see https://example.com/?utm_source=x&id=7", "see https://example.com/?id=7"
	var calls []string
	t.Cleanup(setPreparePaste(func(kind string, src *Doc, formats map[string]any) *Doc {
		calls = append(calls, fmt.Sprintf("%s %v", kind, formats))
		if !strings.Contains(src.Text(), "utm_source") {
			return nil
		}
		d := NewDoc()
		d.Insert(0, strings.ReplaceAll(src.Text(), "utm_source=x&", ""), Attr{})
		return d
	}))
	ed := NewEditor()
	ed.paste(dirty)
	if got := ed.Doc().Text(); got != clean {
		t.Errorf("rich paste: %q, want %q", got, clean)
	}
	ed = NewEditor()
	setTautKind(ed, "thread")
	ed.plainPaste = true // Shift: paste and match style
	ed.paste(dirty)
	if got := ed.Doc().Text(); got != clean {
		t.Errorf("plain paste: %q, want %q", got, clean)
	}
	ed = NewEditor()
	ed.Doc().Lines[0].Kind = CodeBlock
	ed.Touch()
	ed.paste("keep https://example.com/?id=7")
	if got := ed.Doc().Text(); got != "keep https://example.com/?id=7" {
		t.Errorf("a paste the hook left: %q", got)
	}
	want := []string{"channel map[]", "thread map[]", "channel map[code-block:true]"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("the hook was asked %q, want %q", calls, want)
	}
}

// pasteThroughDelta hands the paste to the clipboard as its Quill ops
// and reads back what came: the text and the link a plugin cleaned; nil
// when it changed nothing.
func TestPasteThroughDelta(t *testing.T) {
	const dirty, clean = "https://example.com/?utm_source=x&id=7", "https://example.com/?id=7"
	src := NewDoc()
	end := src.Insert(0, "see ", Attr{})
	end = src.Insert(end, dirty, Attr{})
	src.SetLink(4, end, dirty)
	got := throughDelta(src, func(ops json.RawMessage) (json.RawMessage, bool) {
		var list []map[string]any
		if err := json.Unmarshal(ops, &list); err != nil {
			t.Fatal(err)
		}
		for _, op := range list {
			if s, ok := op["insert"].(string); ok {
				op["insert"] = strings.ReplaceAll(s, "utm_source=x&", "")
			}
			if at, ok := op["attributes"].(map[string]any); ok {
				if l, ok := at["link"].(string); ok {
					at["link"] = strings.ReplaceAll(l, "utm_source=x&", "")
				}
			}
		}
		out, _ := json.Marshal(list)
		return out, true
	})
	if got == nil {
		t.Fatal("no Doc from the plugin's ops")
	}
	if got.Text() != "see "+clean {
		t.Errorf("text %q, want %q", got.Text(), "see "+clean)
	}
	if _, _, u := got.LinkAt(6); u != clean {
		t.Errorf("link %q, want %q", u, clean)
	}
	if d := throughDelta(src, func(ops json.RawMessage) (json.RawMessage, bool) { return ops, false }); d != nil {
		t.Errorf("an unchanged paste came back as %q", d.Text())
	}
}

// cleanOnPaste patches Slack's composer clipboard as Slick's ClearURLs
// does: on a composer's focusin, through the clipboard module's prototype,
// taking utm_ parameters out of what's pasted.
const cleanOnPaste = `(() => {
const TautPlugin = globalThis.TautPlugin
class CleanOnPaste extends TautPlugin {
  static id = 'CleanOnPaste'
  static pluginName = 'Clean On Paste'
  static description = 'patches the composer clipboard on focusin, as ClearURLs does (test)'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    this.focus = (e) => {
      const c = e.target && e.target.closest && e.target.closest('.ql-container')
      const cb = c && c.__quill && c.__quill.getModule('clipboard')
      const proto = cb && Object.getPrototypeOf(cb)
      if (!proto || typeof proto.preparePastedDelta !== 'function' || this.restore) return
      const prepare = proto.preparePastedDelta
      const wrapped = function (args) {
        const ops = args.pastedDelta.ops.map((op) => typeof op.insert === 'string' ? { ...op, insert: op.insert.replace(/utm_[a-z]+=[^&\s]*&?/g, '') } : op)
        return prepare.call(this, { ...args, pastedDelta: new args.pastedDelta.constructor(ops) })
      }
      proto.preparePastedDelta = wrapped
      this.restore = () => { if (proto.preparePastedDelta === wrapped) proto.preparePastedDelta = prepare }
    }
    document.addEventListener('focusin', this.focus, true)
  }
  stop() {
    document.removeEventListener('focusin', this.focus, true)
    if (this.restore) this.restore()
    this.restore = undefined
  }
}
return CleanOnPaste
})()`

// The whole path with Taut: the composer's focus reaches the stand-in
// Quill, the plugin patches its clipboard there, the paste hook comes on
// (syncHooks, every composer frame), and a pasted link goes in as the
// plugin made it.
func TestTautPasteClipboard(t *testing.T) {
	startTautCode(t, []byte(cleanOnPaste), nil, nil)
	const dirty, clean = "see https://example.com/?utm_source=x&id=7", "see https://example.com/?id=7"
	waitFor(t, 10*time.Second, "the clipboard patched on the composer's focus", func() bool {
		syncHooks() // what every composer frame does
		focusComposer("channel", true)
		return pasteHookOn()
	})
	ed := NewEditor()
	ed.paste(dirty)
	if got := ed.Doc().Text(); got != clean {
		t.Errorf("pasted %q, want %q", got, clean)
	}
}
