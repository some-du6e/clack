package slackc

import "gioui.org/io/key"

// hostBox is the layout box of the component's "@" node (nil outside a
// laid-out tree).
func (h *Host) hostBox() *box {
	if h.view == nil || h.Node == nil || h.Node.K == 0 {
		return nil
	}
	return h.view.byK[h.Node.K]
}

// DispatchKey sends a DOM keydown/keyup (from e.State) at the component's
// host node, so it bubbles through the plugin's own elements around it
// (api.modal's wrapper div, a form's onKeyDown) like a key in a browser
// input. Call it alongside the component's own onKeyDown prop.
func (h *Host) DispatchKey(e key.Event) {
	b := h.hostBox()
	if b == nil {
		return
	}
	typ := "keydown"
	if e.State == key.Release {
		typ = "keyup"
	}
	h.view.dispatchKey(b, typ, e)
}

// SubmitForm is a text field's implicit submission (Enter): a "submit"
// event at the nearest <form> around the component. It reports whether
// there was one.
func (h *Host) SubmitForm() bool {
	b := h.hostBox()
	if b == nil {
		return false
	}
	f := formOf(b)
	if f == nil {
		return false
	}
	h.view.dispatchSimple(f, "submit", nil)
	return true
}
