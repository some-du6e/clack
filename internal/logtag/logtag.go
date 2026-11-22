// Package logtag writes Slack ids into clack.log as docs/INSPECT.md's
// tags: a kind and the first six hex digits of SHA-256 over a salt and
// the id ("ch:3fa2b1", "ts:9c01de"). The salt is random, one per run,
// and never written anywhere, so within one log the same conversation
// or message keeps its tag ("the same message", "another one"), and
// nothing in a line names one. Every log line a navigation writes (the
// thread opens, jumpToMessage, the history, tab and member reads that
// follow) tags its ids with it, so no line beside a tag gives the id
// away.
package logtag

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
)

var salt = sync.OnceValue(func() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("logtag: no randomness for the log's salt: " + err.Error())
	}
	return b
})

// ID is id's tag, its kind read off the id itself (C/G channel, D DM,
// U/W user, B bot, T/E team, F file, a timestamp ts); "-" for none.
func ID(id string) string {
	if id == "" {
		return "-"
	}
	kind := idKind(id)
	h := sha256.New()
	h.Write(salt())
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(id))
	return kind + ":" + hex.EncodeToString(h.Sum(nil))[:6]
}

// IDs is ID for each id, space-separated.
func IDs(ids []string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = ID(id)
	}
	return strings.Join(out, " ")
}

var (
	slackID = regexp.MustCompile(`^[A-Z][A-Z0-9]{6,}$`)
	slackTS = regexp.MustCompile(`^\d{9,11}\.\d{1,6}$`)
)

// idKind is inspect's (internal/ui/inspect redact.go): one id, one kind.
func idKind(id string) string {
	if !slackID.MatchString(id) || !strings.ContainsAny(id, "0123456789") {
		if slackTS.MatchString(id) {
			return "ts"
		}
		return "id"
	}
	switch id[0] {
	case 'C', 'G':
		return "ch"
	case 'D':
		return "dm"
	case 'U', 'W':
		return "user"
	case 'B':
		return "bot"
	case 'T', 'E':
		return "team"
	case 'F':
		return "file"
	}
	return "id"
}
