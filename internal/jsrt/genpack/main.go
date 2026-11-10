// Command genpack writes internal/jsrt/assets.pack from the engine and the
// boot scripts (run by "go generate ./internal/jsrt", from that directory).
package main

import (
	"log"
	"os"

	"github.com/some-du6e/clack/internal/jsrt"
)

func main() {
	files := map[string][]byte{}
	for _, name := range jsrt.PackFiles {
		b, err := os.ReadFile(name)
		if err != nil {
			log.Fatal(err)
		}
		files[name] = b
	}
	pack, err := jsrt.WritePack(files)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("assets.pack", pack, 0o644); err != nil {
		log.Fatal(err)
	}
}
