package quip

// cardFields reads a link card section's card through Content.LinkUnfurl
// (a red tree swaps this file for the old code, where the card stays raw
// in Content.Other).
func cardFields(s *Section) (url string, typ int32, unfurlID string, unfurled bool) {
	if s.Content == nil || s.Content.LinkUnfurl == nil {
		return "", 0, "", false
	}
	c := s.Content.LinkUnfurl
	return str(c.URL), c.CardType(), c.UnfurlID(), c.IsUnfurled != nil && *c.IsUnfurled
}
