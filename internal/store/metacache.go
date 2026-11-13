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

// readDiskCache decodes meta.json from r into d, except the users, which
// go to user one by one. Any error leaves the caller to discard what was
// read: the cache loads whole or not at all.
func readDiskCache(r io.Reader, d *diskCache, user func(*slack.User)) error {
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		switch key {
		case "users":
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			if tok == nil { // null
				continue
			}
			if dl, ok := tok.(json.Delim); !ok || dl != '[' {
				return fmt.Errorf("meta.json: users is %v, not a list", tok)
			}
			for dec.More() {
				u := new(slack.User)
				if err := dec.Decode(u); err != nil {
					return err
				}
				user(u)
			}
			if err := expectDelim(dec, ']'); err != nil {
				return err
			}
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
		default:
			var skip json.RawMessage
			err = dec.Decode(&skip)
		}
		if err != nil {
			return err
		}
	}
	return expectDelim(dec, '}')
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
	put(`{"users":[`)
	for i, u := range users {
		if i > 0 {
			put(",")
		}
		val(u)
	}
	put("]")
	if len(d.Emoji) > 0 {
		put(`,"emoji":`)
		val(d.Emoji)
	}
	if d.EmojiPacked != "" {
		put(`,"emoji_packed":`)
		val(d.EmojiPacked)
	}
	if d.EmojiAt != 0 {
		put(`,"emoji_at":`)
		val(d.EmojiAt)
	}
	if len(d.Usergroups) > 0 {
		put(`,"usergroups":`)
		val(d.Usergroups)
	}
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
