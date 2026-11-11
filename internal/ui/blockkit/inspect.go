package blockkit

import "image"

// Read-only views of blockkit's state and the user's modal actions, for
// tests and debugging (UI goroutine only).

// ModalInfo describes the open modal.
type ModalInfo struct {
	Depth                  int // views on the stack
	ViewID, Title          string
	Submit, Close          string            // footer labels as drawn
	Errors                 map[string]string // block_id → message under the input
	Banner                 string            // the error banner, "" for none
	Retry, Busy, BackShown bool
}

func openStack(h Host) *Stack {
	appsMu.Lock()
	a := apps[h]
	appsMu.Unlock()
	if a == nil || a.stack == nil || a.stack.Dismissed() || len(a.stack.views) == 0 {
		return nil
	}
	return a.stack
}

// OpenModal reports the open modal, if any.
func OpenModal(h Host) (ModalInfo, bool) {
	s := openStack(h)
	if s == nil {
		return ModalInfo{}, false
	}
	sf := s.top()
	v := sf.view
	mi := ModalInfo{Depth: len(s.views), ViewID: v.ID, Title: titleOf(v.Title), Errors: map[string]string{},
		Banner: s.banner, Retry: s.bannerRetry, Busy: s.busy, BackShown: len(s.views) > 1,
		Close: firstNonEmpty(titleOf(v.Close), "Close")}
	if v.Submit != nil {
		mi.Submit = firstNonEmpty(titleOf(v.Submit), "Submit")
	}
	for k, m := range sf.errs {
		mi.Errors[k] = m
	}
	return mi, true
}

// FieldInfo is one text input of the open modal's top view.
type FieldInfo struct {
	BlockID, ActionID, Type string
	Text                    string          // what the field holds
	Value                   string          // its state value as views.submit sends it ("" for null)
	Focused                 bool            // it had the key focus in the last frame
	R                       image.Rectangle // window coordinates, as last drawn
}

// ModalFields lists the text inputs of the open modal's top view, in
// block order, as of the last frame.
func ModalFields(h Host) []FieldInfo {
	s := openStack(h)
	if s == nil {
		return nil
	}
	return s.fields()
}

func (s *Stack) fields() []FieldInfo {
	if len(s.views) == 0 {
		return nil
	}
	sf := s.top()
	var out []FieldInfo
	for i := range sf.blocks {
		b := &sf.blocks[i]
		if b.Type != "input" || b.Element == nil || !isTextInput(b.Element.Type) {
			continue
		}
		e := b.Element
		w := sf.ws[b.BlockID+"\x00"+e.ActionID+"\x00"+e.Type]
		if w == nil {
			continue
		}
		fi := FieldInfo{BlockID: b.BlockID, ActionID: e.ActionID, Type: e.Type, Focused: w.focusedD}
		v := sf.vals.get(b.BlockID, e.ActionID)
		switch {
		case w.ed != nil:
			fi.Text = w.ed.Text()
		case w.red != nil:
			fi.Text = w.red.Doc().Text()
		}
		if v != nil {
			if e.Type == "rich_text_input" {
				fi.Value = richPlain(v.Rich)
			} else {
				fi.Value = textValue(e.Type, v.Text)
			}
		}
		org := sf.org.Add(w.pos)
		fi.R = image.Rectangle{Min: org, Max: org.Add(w.box)}
		out = append(out, fi)
	}
	return out
}

// SubmitModal is the user pressing the modal's submit button.
func SubmitModal(h Host) {
	if s := openStack(h); s != nil {
		s.doSubmit()
	}
}

// BackModal is the footer's close button (or the back arrow, or Escape).
func BackModal(h Host) {
	if s := openStack(h); s != nil {
		s.userBack()
	}
}

// CloseModal is the header's ×.
func CloseModal(h Host) {
	if s := openStack(h); s != nil {
		s.userCloseAll()
	}
}

// HomeHash is the hash of the App Home view the Home tab shows ("" for none).
func (d *AppDM) HomeHash() string {
	if d.surf == nil || d.surf.view == nil {
		return ""
	}
	return d.surf.view.Hash
}
