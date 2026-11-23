package popups

// The search relatives tests' reads (search_relatives_test.go).

// sicCardsT is search in channel's result cards.
func sicCardsT(v *SearchInChannel) []*srCard {
	if v.res == nil {
		return nil
	}
	return v.res.cards
}
