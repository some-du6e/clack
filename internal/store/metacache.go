package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/some-du6e/clack/internal/slack"
)

// meta.json streams in and out one value at a time. At the Hack Club
// account's scale (tens of thousands of users) it is tens of megabytes:
// reading it whole, then unmarshalling it into a slice of every user that
// PutUsers copies again, held three copies of the directory at once at
// start, and every save (each 5 minutes after any change) built the whole
// file in memory from yet another copy of every user. The file's content
// is the same as before (diskCache's keys and values).

// metaLayout is the key order writeDiskCache uses: what a launch's first
// frame reads (the prefs, the channel names, the bots, the experiments,
// the users) first, then the custom emoji and the user groups (6.6 MB and
// 0.4 MB on Hack Club), which a launch reads after its first frame
// (LoadCacheBase; Slack loads custom emoji after its first render too).
// Files from before it (no "layout" key) put the emoji first; they still
// load, all at once, until the next save rewrites them.
const metaLayout = 2

// restKey reports whether key belongs to the part a launch reads after
// its first frame.
func restKey(key string) bool {
	switch key {
	case "emoji", "emoji_packed", "emoji_at", "usergroups":
		return true
	}
	return false
}

// metaDecoder reads meta.json in two parts: base up to the first restKey
// (in a layout-2 file every other key comes before it), rest the keys
// from there to the end. A file without the layout key is read whole by
// base. Errors leave the caller to discard what the part read.
type metaDecoder struct {
	dec     *json.Decoder
	d       *diskCache
	user    func(*slack.User)
	layout  int
	pending string // the restKey base stopped at ("": base read to the end)
	done    bool   // the closing brace was read
}

func newMetaDecoder(r io.Reader, d *diskCache, user func(*slack.User)) *metaDecoder {
	return &metaDecoder{dec: json.NewDecoder(r), d: d, user: user}
}

// base reads the opening brace and the keys before the first restKey.
func (m *metaDecoder) base() error {
	if err := expectDelim(m.dec, '{'); err != nil {
		return err
	}
	for m.dec.More() {
		tok, err := m.dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if m.layout >= metaLayout && restKey(key) {
			m.pending = key // its value is the next token: rest decodes it
			return nil
		}
		if err := m.value(key); err != nil {
			return err
		}
	}
	m.done = true
	return expectDelim(m.dec, '}')
}

// rest reads what base left: the pending key's value, the keys after it
// and the closing brace.
func (m *metaDecoder) rest() error {
	if m.done {
		return nil
	}
	if m.pending != "" {
		if err := m.value(m.pending); err != nil {
			return err
		}
		m.pending = ""
	}
	for m.dec.More() {
		tok, err := m.dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if err := m.value(key); err != nil {
			return err
		}
	}
	m.done = true
	return expectDelim(m.dec, '}')
}

// value decodes key's value into d (the users one by one to user).
func (m *metaDecoder) value(key string) error {
	dec, d := m.dec, m.d
	var err error
	switch key {
	case "layout":
		err = dec.Decode(&m.layout)
	case "users":
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if tok == nil { // null
			return nil
		}
		if dl, ok := tok.(json.Delim); !ok || dl != '[' {
			return fmt.Errorf("meta.json: users is %v, not a list", tok)
		}
		for dec.More() {
			u := new(slack.User)
			if err := dec.Decode(u); err != nil {
				return err
			}
			m.user(u)
		}
		return expectDelim(dec, ']')
	case "emoji":
		err = dec.Decode(&d.Emoji)
	case "emoji_packed":
		err = dec.Decode(&d.EmojiPacked)
	case "emoji_at":
		err = dec.Decode(&d.EmojiAt)
	case "usergroups":
		err = dec.Decode(&d.Usergroups)
	case "prefs":
		err = dec.Decode(&d.Prefs)
	case "channel_names":
		err = dec.Decode(&d.Names)
	case "bots":
		err = dec.Decode(&d.Bots)
	case "user_seen":
		err = dec.Decode(&d.UserSeen)
	case "experiments":
		err = dec.Decode(&d.Experiments)
	case "features":
		err = dec.Decode(&d.Features)
	case "enterprise_prefs":
		err = dec.Decode(&d.EnterprisePrefs)
	case "notifications_max_ts":
		err = dec.Decode(&d.NotificationsMaxTS)
	default:
		var skip json.RawMessage
		err = dec.Decode(&skip)
	}
	return err
}

// readDiskCache decodes meta.json from r into d whole, except the users,
// which go to user one by one. Any error leaves the caller to discard
// what was read: the cache loads whole or not at all.
func readDiskCache(r io.Reader, d *diskCache, user func(*slack.User)) error {
	m := newMetaDecoder(r, d, user)
	if err := m.base(); err != nil {
		return err
	}
	return m.rest()
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if dl, ok := tok.(json.Delim); !ok || dl != want {
		return fmt.Errorf("meta.json: %v where %v belongs", tok, want)
	}
	return nil
}

// writeDiskCache writes d to w with users in place of d.Users, one user
// at a time (json.Marshal of the whole diskCache wrote the same keys).
func writeDiskCache(w io.Writer, d *diskCache, users []*slack.User) error {
	bw := bufio.NewWriterSize(w, 64<<10)
	enc := json.NewEncoder(bw) // escapes HTML as json.Marshal does
	var err error
	put := func(s string) {
		if err == nil {
			_, err = bw.WriteString(s)
		}
	}
	val := func(v any) {
		if err == nil {
			err = enc.Encode(v)
		}
	}
	// layout 2 (metaLayout): what a first frame reads first, the custom
	// emoji and the user groups last
	put(`{"layout":`)
	val(metaLayout)
	if d.Prefs != nil {
		put(`,"prefs":`)
		val(d.Prefs)
	}
	if len(d.Names) > 0 {
		put(`,"channel_names":`)
		val(d.Names)
	}
	if len(d.Bots) > 0 {
		put(`,"bots":`)
		val(d.Bots)
	}
	if len(d.UserSeen) > 0 {
		put(`,"user_seen":`)
		val(d.UserSeen)
	}
	if d.Experiments != nil {
		put(`,"experiments":`)
		val(d.Experiments)
	}
	if d.Features != nil {
		put(`,"features":`)
		val(d.Features)
	}
	if len(d.EnterprisePrefs) > 0 { // before the first frame, as Slack's persisted store
		put(`,"enterprise_prefs":`)
		val(d.EnterprisePrefs)
	}
	if d.NotificationsMaxTS != "" {
		put(`,"notifications_max_ts":`)
		val(d.NotificationsMaxTS)
	}
	put(`,"users":[`)
	for i, u := range users {
		if i > 0 {
			put(",")
		}
		val(u)
	}
	put("]")
	if d.EmojiAt != 0 {
		put(`,"emoji_at":`)
		val(d.EmojiAt)
	}
	if len(d.Usergroups) > 0 {
		put(`,"usergroups":`)
		val(d.Usergroups)
	}
	if len(d.Emoji) > 0 {
		put(`,"emoji":`)
		val(d.Emoji)
	}
	if d.EmojiPacked != "" {
		put(`,"emoji_packed":`)
		val(d.EmojiPacked)
	}
	put("}")
	if err != nil {
		return err
	}
	return bw.Flush()
}

// writeAtomicStream is writeAtomic with the content written by write.
func writeAtomicStream(dir, name string, write func(io.Writer) error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+"-*.tmp") // 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
