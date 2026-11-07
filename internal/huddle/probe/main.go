// Command probe joins one huddle headlessly to test internal/huddle live: a
// test tone for the microphone, logs of participants, speaking, received
// audio and video, then it leaves. A developer tool; not shipped.
//
//	CLACK_HUDDLE_TONE=440 probe -live -channel C0B4DEFKFCJ -for 90s
//
// Starting a huddle rings people: don't run it without the channel's
// owner agreeing. Tests use the fake Chime server (fake_test.go).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

func main() {
	home, _ := os.UserConfigDir()
	credsPath := flag.String("creds", filepath.Join(home, "clack", "creds.json"), "credentials")
	channel := flag.String("channel", "C0B4DEFKFCJ", "channel or DM to huddle in")
	dur := flag.Duration("for", 60*time.Second, "how long to stay")
	react := flag.String("react", "wave", "reaction to send after 10s (\"\" = none)")
	live := flag.Bool("live", false, "really start or join a live huddle (it rings people in the channel)")
	flag.Parse()
	if !*live {
		log.Fatal("probe: refusing to start a live huddle without -live (it rings the channel's members; test against fakes instead)")
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	if os.Getenv("CLACK_HUDDLE_TONE") == "" {
		os.Setenv("CLACK_HUDDLE_TONE", "440")
	}
	b, err := os.ReadFile(*credsPath)
	if err != nil {
		log.Fatal(err)
	}
	var creds slack.Creds
	if err := json.Unmarshal(b, &creds); err != nil {
		log.Fatal(err)
	}
	c := slack.New(creds)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	s, err := huddle.Join(ctx, huddle.Options{
		Client: c, SelfID: creds.UserID,
		OnPeer: func(ev huddle.PeerEvent) {
			if ev.Kind != "draw" {
				j, _ := json.Marshal(ev)
				log.Printf("peer %s: %s", ev.Topic, j)
			}
		},
		OnEnd: func(err error) { log.Printf("ended: %v", err) },
	}, *channel)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	st := s.State()
	log.Printf("room %s thread %s/%s", st.RoomID, st.ThreadChannel, st.ThreadTS)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	end := time.After(*dur)
	tick := time.NewTicker(2 * time.Second)
	reacted := time.After(10 * time.Second)
	last := ""
	for {
		select {
		case <-sig:
			s.Leave()
			return
		case <-end:
			log.Printf("leaving: %s", s.Stats())
			s.Leave()
			time.Sleep(300 * time.Millisecond)
			return
		case <-s.Done():
			log.Printf("session done: %v", s.State().Err)
			return
		case <-reacted:
			if *react != "" {
				log.Printf("react %s: %v", *react, s.React(*react, ""))
			}
		case <-tick.C:
			st := s.State()
			now := time.Now()
			var ps []string
			for _, p := range st.Participants {
				f := ""
				if p.InMedia {
					f += "A"
				}
				if p.Muted {
					f += "M"
				}
				if p.Speaking(now) {
					f += "S"
				}
				if p.Camera {
					f += "C"
				}
				if p.Sharing {
					f += "D"
				}
				if p.Reaction != "" && now.Sub(p.ReactedAt) < 5*time.Second {
					f += ":" + p.Reaction
				}
				ps = append(ps, fmt.Sprintf("%s[%s %.2f]", p.UserID, f, p.Volume))
			}
			var vs []string
			for _, v := range st.Videos {
				fr := s.Frame(v.Key)
				desc := fmt.Sprintf("%s %dx%d seq %d", v.Key, v.W, v.H, fr.Seq)
				if fr.Img != nil {
					desc += fmt.Sprintf(" mean %s", mean(fr))
				}
				vs = append(vs, desc)
			}
			line := fmt.Sprintf("%s | %s | videos: %s | %s", st.Phase, strings.Join(ps, " "), strings.Join(vs, "; "), s.Stats())
			if line != last {
				log.Print(line)
				last = line
			}
		}
	}
}

// mean is the average colour of a frame (to check decoding without saving it).
func mean(f huddle.VideoFrame) string {
	var r, g, b, n uint64
	p := f.Img.Pix
	for i := 0; i+3 < len(p); i += 4 * 97 {
		r += uint64(p[i])
		g += uint64(p[i+1])
		b += uint64(p[i+2])
		n++
	}
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("#%02x%02x%02x", r/n, g/n, b/n)
}
