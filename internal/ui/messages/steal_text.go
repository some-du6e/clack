package messages

import "gioui.org/io/key"

// StealText gathers what a frame's keys hand to a conversation's input
// (shouldInputStealKeyEvent's keys: the App's with nothing focused, a
// message list's with a message selected), to type it there at once.
//
// Typed at once, not key by key: the first key's FocusCmd would replay
// the frame's later EditEvents to the input with the ranges the window
// gave them for its empty IME model ({1,1}, {2,2}…), which the input
// takes as replacements of its own text ("hello" + "a", "b" became
// "hbelloa"). From the first stolen key on, the frame's text is the
// input's whatever it is (Space too), as in Slack, where the input has the
// focus once the first key's keydown is through.
//
// An IME's or a dead key's marked text (a CompositionEvent over the text
// just typed) isn't typed: the composition goes on where the text was,
// and its committed text comes as an EditEvent with the composition
// closed ({-1,-1}), which is. Slack's isPrintableKeyEvent sees "Process"
// or "Dead" for those keys and steals nothing.
type StealText struct {
	composing bool        // a composition is open (across frames)
	started   bool        // a key went to the input this frame
	text      []byte      // the frame's text, committed
	pending   []byte      // text since the last composition boundary
	keys      []key.Event // the frame's shortcuts (Mod+B, Mod+I, Mod+V)
}

// Edit takes typed text; first says whether a frame's first key steals
// (stealsText, stealsKey). A composition's text always goes in pending,
// which its CompositionEvent then drops or keeps.
func (s *StealText) Edit(e key.EditEvent, first func(string) bool) {
	if e.Text == "" {
		return
	}
	if !s.composing && !s.started && !first(e.Text) {
		return
	}
	s.pending = append(s.pending, e.Text...)
	s.started = true
}

// Composition takes a CompositionEvent: a range over text marks the text
// typed since the last boundary as an IME's, still being composed (not
// typed); an empty one ({-1,-1}) closes it, and the text before it was
// committed.
func (s *StealText) Composition(e key.CompositionEvent) {
	if e.Start >= 0 && e.End > e.Start {
		s.composing, s.pending = true, s.pending[:0]
		s.started = len(s.text) > 0 || len(s.keys) > 0
		return
	}
	s.composing = false
	s.text = append(s.text, s.pending...)
	s.pending = s.pending[:0]
}

// Key takes a shortcut the input does (Mod+B, Mod+I, Mod+V).
func (s *StealText) Key(k key.Event) {
	s.keys = append(s.keys, k)
	s.started = true
}

// Started reports whether a key went to the input this frame: the
// frame's other keys are then the input's too.
func (s *StealText) Started() bool { return s.started }

// Take returns the frame's text and shortcuts for the input and starts
// the next frame. Text still being composed stays out.
func (s *StealText) Take() (text string, keys []key.Event) {
	if !s.composing {
		s.text = append(s.text, s.pending...)
	}
	text, keys = string(s.text), s.keys
	s.text, s.pending, s.keys, s.started = s.text[:0], s.pending[:0], nil, false
	return text, keys
}
