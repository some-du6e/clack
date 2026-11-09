package huddleui

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
)

// The invite sound. Slack's playHuddleInviteSound (client-boot ws4B) plays
// the file the huddle_invite_sound_v2 pref names, once (no loop), on the
// default output, unless the mute_huddle_sounds pref is on
// (canPlayHuddleSound) or you're in Do Not Disturb; an unset pref or
// "none" is silence. The names are Slack's notification sounds (rpMl's $O
// and N7); the files are its public CDN assets (BLe4), fetched without
// any cookie and cached under the user cache directory.

// slackSoundCDN is the build's asset path (webpack's publicPath).
const slackSoundCDN = "https://a.slack-edge.com/bv1-13-br/"

// slackSounds maps a sound pref value to its CDN file (BLe4).
var slackSounds = map[string]string{
	"b2.mp3":                         "b2-5fd58e3.mp3",                         // Ding
	"animal_stick.mp3":               "animal_stick-caaec1f.mp3",               // Boing
	"been_tree.mp3":                  "been_tree-6428cfb.mp3",                  // Drop
	"complete_quest_requirement.mp3": "complete_quest_requirement-74d22f6.mp3", // Ta-da
	"confirm_delivery.mp3":           "confirm_delivery-7da5529.mp3",           // Plink
	"flitterbug.mp3":                 "flitterbug-a658890.mp3",                 // Wow
	"here_you_go_lighter.mp3":        "here_you_go_lighter-313ec4e.mp3",        // Here you go
	"hi_flowers_hit.mp3":             "hi_flowers_hit-8fa995c.mp3",             // Hi
	"knock_brush.mp3":                "knock_brush-ac2b6e8.mp3",                // Knock Brush
	"save_and_checkout.mp3":          "save_and_checkout-3792bb8.mp3",          // Whoa!
	"item_pickup.mp3":                "item_pickup-b0e3113.mp3",                // Yoink
	"hummus.mp3":                     "hummus-2e2613c.mp3",                     // Hummus
	"boop.mp3":                       "boop-0ccd0e9.mp3",                       // Boop
	"boop_remix.mp3":                 "boop_remix-fbf4aad.mp3",                 // Boop Plus
}

type ringState struct {
	mu   sync.Mutex
	stop chan struct{}
}

// InviteSound is the file an invite would ring with now ("" = silent):
// huddle_invite_sound_v2, unless mute_huddle_sounds.
func (c *Controller) InviteSound() string {
	app := c.h.Core()
	if app == nil || app.PrefBool("mute_huddle_sounds", false) {
		return ""
	}
	f := app.PrefString("huddle_invite_sound_v2")
	if f == "none" {
		return ""
	}
	return f
}

// ringInvite plays the invite sound once. Callers checked DND and that
// you're not in a huddle.
func (c *Controller) ringInvite() {
	file := c.InviteSound()
	if file == "" {
		return
	}
	if c.Ring != nil {
		c.Ring(file)
		return
	}
	cdn, ok := slackSounds[file]
	if !ok {
		return
	}
	stop := make(chan struct{})
	c.ring.mu.Lock()
	if c.ring.stop != nil {
		close(c.ring.stop)
	}
	c.ring.stop = stop
	c.ring.mu.Unlock()
	go func() {
		pcm, err := loadSound(cdn)
		if err != nil {
			log.Printf("huddle: invite sound %s: %v", file, err)
			return
		}
		select {
		case <-stop:
			return
		default:
		}
		if err := huddle.PlaySound(pcm, stop, log.Printf); err != nil {
			log.Printf("huddle: invite sound: %v", err)
		}
	}()
}

// stopRing stops the invite sound (respondToHuddleInvite and a cleared
// invite stop it).
func (c *Controller) stopRing() {
	c.ring.mu.Lock()
	if c.ring.stop != nil {
		close(c.ring.stop)
		c.ring.stop = nil
	}
	c.ring.mu.Unlock()
}

var sounds struct {
	mu  sync.Mutex
	pcm map[string][]int16
}

// loadSound returns a CDN sound as 48 kHz stereo PCM (memory, then disk,
// then the CDN).
func loadSound(file string) ([]int16, error) {
	sounds.mu.Lock()
	if p, ok := sounds.pcm[file]; ok {
		sounds.mu.Unlock()
		return p, nil
	}
	sounds.mu.Unlock()
	b, err := soundBytes(file)
	if err != nil {
		return nil, err
	}
	pcm, err := decodeMP3(b)
	if err != nil {
		return nil, err
	}
	sounds.mu.Lock()
	if sounds.pcm == nil {
		sounds.pcm = map[string][]int16{}
	}
	sounds.pcm[file] = pcm
	sounds.mu.Unlock()
	return pcm, nil
}

func soundBytes(file string) ([]byte, error) {
	if strings.ContainsAny(file, `/\`) || !strings.HasSuffix(file, ".mp3") {
		return nil, errors.New("bad sound name")
	}
	var path string
	if dir, err := os.UserCacheDir(); err == nil {
		path = filepath.Join(dir, "clack", "sounds", file)
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return b, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, slackSoundCDN+file, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req) // a public asset: no cookie, no token
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("CDN: " + res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if path != "" && os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		tmp := path + ".part"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	return b, nil
}

// decodeMP3 decodes to 48 kHz interleaved stereo.
func decodeMP3(b []byte) ([]int16, error) {
	d, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(d) // 16-bit little-endian stereo
	if err != nil && len(raw) == 0 {
		return nil, err
	}
	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return huddle.ResampleStereo(pcm, d.SampleRate()), nil
}
