package store

// ConvKind is richtext.ConvKinder: the kind of conversation id ("im",
// "mpim", "private" or "public"), ok false when the store doesn't know
// it (message permalink pills and unfurl cards: Slack's "Private message"
// and "From a private conversation"). A conversation only known by name
// (channel previews, search) counts as public.
func (s *Store) ConvKind(id string) (kind string, ok bool) {
	s.mu.RLock()
	c, found := s.Convs[id]
	if !found {
		c, found = s.ext[id]
	}
	_, named := s.names[id]
	s.mu.RUnlock()
	switch {
	case found && c.IsIM:
		return "im", true
	case found && c.IsMPIM:
		return "mpim", true
	case found && (c.IsPrivate || c.IsGroup):
		return "private", true
	case found || named:
		return "public", true
	}
	if id != "" {
		s.noteMissing('c', id)
	}
	return "", false
}
