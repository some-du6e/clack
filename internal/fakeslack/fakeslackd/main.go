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

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8930", "listen address (loopback)")
	creds := flag.String("creds", "", "write credentials for the fake here")
	small := flag.Bool("small", false, "small workspace instead of Hack Club sized")
	gallery := flag.Bool("gallery", false, "add #blockkit-gallery (every Block Kit fixture)")
	flag.Parse()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	sz := fakeslack.HackClub
	if *small {
		sz = fakeslack.Small
	}
	sz.Gallery = *gallery
	s := fakeslack.Serve(ln, sz)
	if *creds != "" {
		if err := s.WriteCreds(*creds); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("fakeslackd: serving %s (%d emoji, %d channels, %d users)", s.URL, sz.Emoji, sz.Channels, sz.Users)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}
