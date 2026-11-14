// Command fakeslackd serves internal/fakeslack on a loopback port and writes
// credentials for it, so the real clack binary can be benchmarked end to
// end (startup to first paint, RSS) without touching Slack:
//
//	fakeslackd -addr 127.0.0.1:8930 -creds $CLACK_HOME/creds.json &
//	CLACK_HOME=... clack
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8930", "listen address (loopback)")
	creds := flag.String("creds", "", "write credentials for the fake here")
	small := flag.Bool("small", false, "small workspace instead of Hack Club sized")
	scale := flag.Bool("scale", false, "the real account's scale (fakeslack.HackClubScale: 66k emoji entries, 32k users, 2k user groups, long channels)")
	userIDs := flag.String("userids", "", "write the directory's user ids here, one per line (the memory workflow's driver loads them all)")
	gallery := flag.Bool("gallery", false, "add #blockkit-gallery (every Block Kit fixture)")
	gifs := flag.Int("gifs", 0, "add #gif-wall with this many messages, each a large animated GIF (the GIF memory test's channel)")
	latency := flag.Duration("latency", 0, "added to every API call (a network round trip), e.g. 150ms")
	imgLatency := flag.Duration("img-latency", 0, "added to every image download")
	iaTheme := flag.String("ia-theme", "", "the account's users.prefs ia_theme (JSON), instead of Slack's default")
	flag.Parse()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	sz := fakeslack.HackClub
	if *small {
		sz = fakeslack.Small
	}
	if *scale {
		sz = fakeslack.HackClubScale
	}
	sz.Gallery = *gallery
	sz.GIFs = *gifs
	s := fakeslack.Serve(ln, sz)
	s.Latency, s.ImgLatency = *latency, *imgLatency // before the first request
	if *iaTheme != "" {
		s.SetPrefs(map[string]any{"ia_theme": *iaTheme})
	}
	if *userIDs != "" {
		if err := os.WriteFile(*userIDs, []byte(strings.Join(s.UserIDs(), "\n")+"\n"), 0o600); err != nil {
			log.Fatal(err)
		}
	}
	if *creds != "" { // last: the harness starts the client once this file exists
		if err := s.WriteCreds(*creds); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("fakeslackd: serving %s (%d emoji, %d channels, %d users, latency %v, images %v)", s.URL, sz.Emoji, sz.Channels, sz.Users, *latency, *imgLatency)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}
