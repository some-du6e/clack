package quip

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
)

// NewSectionID is ZRIn.generateEntityId: "temp:C:", the document's shard
// characters, then the first 25 hex digits of a random v4 UUID without
// its dashes (s6aU.uuid). The server keeps "temp:C:" ids as they are
// (2U5W.isNewSectionId).
func NewSectionID(shard string) string {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		panic("quip: no randomness: " + err.Error())
	}
	u[6] = u[6]&0x0f | 0x40 // version 4
	u[8] = u[8]&0x3f | 0x80 // RFC 4122 variant
	return "temp:C:" + shard + hex.EncodeToString(u[:])[:25]
}

// newSectionIDRE is what NewSectionID makes (and the server keeps).
var newSectionIDRE = regexp.MustCompile(`^temp:C:[A-Za-z0-9]{3}[0-9a-f]{25}$`)

// IsNewSectionID reports whether id is one NewSectionID could have made
// for shard.
func IsNewSectionID(id, shard string) bool {
	return newSectionIDRE.MatchString(id) && strings.HasPrefix(id[len("temp:C:"):], shard)
}

// ShardChars is 7DQG.shardChars: the document's shard_chars, else the
// first three characters of its id (2U5W.getShardCharsFromId).
func ShardChars(d *Document) string {
	if d == nil {
		return ""
	}
	if d.ShardChars != nil && *d.ShardChars != "" {
		return *d.ShardChars
	}
	id := str(d.ID)
	if len(id) > 3 {
		return id[:3]
	}
	return id
}

// SessionID is 7yDq's session id: three of tAAR.s4's four hex digits.
func SessionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("quip: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Syncer ids of Slack objects (2U5W.syncerIdForSlackObject: the prefix
// once, whatever the input already has).
func syncerID(prefix, id string) string {
	if strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + id
}

// UserID is a person's or a group's control id ("su:U…", "su:S…").
func UserID(id string) string { return syncerID("su:", id) }

// ChannelID is a channel's ("sc:C…").
func ChannelID(id string) string { return syncerID("sc:", id) }

// FileID is a file's ("sf:F…").
func FileID(id string) string { return syncerID("sf:", id) }

// EmojiID is 2U5W.syncerIdForSlackEmoji: "se:<name>/<top-level team>".
func EmojiID(name, team string) string { return "se:" + name + "/" + team }

// SlackID is a syncer id's Slack id (the part after su:, sc:, sf:), or
// the id itself.
func SlackID(syncer string) string {
	for _, p := range []string{"su:", "sc:", "sf:", "sd:"} {
		if strings.HasPrefix(syncer, p) {
			return syncer[len(p):]
		}
	}
	return syncer
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Ptr is a field's value: quip.Ptr("x"), quip.Ptr(int32(5)).
func Ptr[T any](v T) *T { return &v }
