package core

// smOf is what TestSearchMatches reads of searchMatches: per result, the
// matched attachments' keys and files' ids (the red evidence run swaps
// this file for a stub).
func smOf(raw []byte) map[[2]int][2]map[string]bool {
	out := map[[2]int][2]map[string]bool{}
	for k, v := range searchMatches(raw) {
		out[k] = [2]map[string]bool{v.Attachments, v.Files}
	}
	return out
}
