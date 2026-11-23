package fakeslack

import (
	"net/url"
	"sync"
)

// contacts.add: a contact_card's Message and Save contact accept its card.
// Synthetic.

// ContactCardRefused is a card id contacts.add refuses with
// cannot_add_contact_card (ContactActionErrorModal's member sentence).
const ContactCardRefused = "CC0REFUSED"

type contactState struct {
	mu   sync.Mutex
	adds []url.Values
}

var contactStates sync.Map // *Server -> *contactState

func (s *Server) contacts() *contactState {
	v, _ := contactStates.LoadOrStore(s, &contactState{})
	return v.(*contactState)
}

// ContactAdds are the contacts.add calls so far, their forms.
func (s *Server) ContactAdds() []url.Values {
	c := s.contacts()
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]url.Values(nil), c.adds...)
}

func (s *Server) handleContacts(method string, f url.Values) (any, bool) {
	if method != "contacts.add" {
		return nil, false
	}
	c := s.contacts()
	c.mu.Lock()
	c.adds = append(c.adds, f)
	c.mu.Unlock()
	switch f.Get("contact_card_id") {
	case "":
		return map[string]any{"ok": false, "error": "invalid_arguments"}, true
	case ContactCardRefused:
		return map[string]any{"ok": false, "error": "cannot_add_contact_card"}, true
	}
	return ok(), true
}
