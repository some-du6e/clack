package spell

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Words added with "Add to Dictionary" live in Chromium's custom
// dictionary file (chrome/browser/spellchecker/spellcheck_custom_dictionary.cc):
// "Custom Dictionary.txt", one word per line, then
// "checksum_v1 = <md5 of the lines>". A file whose checksum doesn't
// match is ignored (Chromium falls back to its .backup). Words are at most
// 99 bytes of UTF-8 with no surrounding whitespace.

const (
	customFile     = "Custom Dictionary.txt"
	checksumPrefix = "checksum_v1 = "
	maxCustomBytes = 99
)

// validCustomWord is IsValidWord.
func validCustomWord(w string) bool {
	return w != "" && len(w) <= maxCustomBytes && utf8.ValidString(w) && strings.TrimSpace(w) == w
}

// loadCustom reads the words from dir's custom dictionary (or its backup
// when the checksum fails).
func loadCustom(dir string) []string {
	if dir == "" {
		return nil
	}
	p := filepath.Join(dir, customFile)
	words, ok := readCustom(p)
	if !ok {
		words, ok = readCustom(p + ".backup")
		if !ok {
			return nil
		}
	}
	return words
}

func readCustom(p string) ([]string, bool) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, os.IsNotExist(err) // a missing file is an empty dictionary
	}
	s := string(b)
	if i := strings.LastIndex(s, checksumPrefix); i >= 0 {
		sum := s[i+len(checksumPrefix):]
		s = s[:i]
		h := md5.Sum([]byte(s))
		if strings.TrimSpace(sum) != hex.EncodeToString(h[:]) {
			return nil, false
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Split(strings.TrimSpace(s), "\n") {
		w = strings.TrimSpace(w)
		if validCustomWord(w) && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out, true
}

// saveCustom writes words (sorted, as Chromium's std::set does) with the
// checksum, keeping the previous file as the backup.
func saveCustom(dir string, words []string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ws := append([]string(nil), words...)
	sort.Strings(ws)
	var b strings.Builder
	for _, w := range ws {
		b.WriteString(w)
		b.WriteByte('\n')
	}
	h := md5.Sum([]byte(b.String()))
	content := b.String() + checksumPrefix + hex.EncodeToString(h[:])
	p := filepath.Join(dir, customFile)
	if _, err := os.Stat(p); err == nil {
		_ = os.Rename(p, p+".backup")
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
