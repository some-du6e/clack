package desktop

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Request is what a second launch hands the running instance.
type Request struct {
	// URL is a slack:// link to open ("" just focuses the window).
	URL string `json:"url,omitempty"`
}

// Instance is the primary (first) Clack process's listener.
type Instance struct {
	ln   net.Listener
	path string
}

// SocketPath is the per-user, per-config-directory socket the instances
// talk over (AF_UNIX works on Linux, macOS and Windows 10 1803+). Separate
// CLACK_HOME directories get separate instances.
func SocketPath(configDir string) string {
	abs, err := filepath.Abs(configDir)
	if err != nil {
		abs = configDir
	}
	h := sha256.Sum256([]byte(abs))
	name := fmt.Sprintf("clack-%x.sock", h[:6])
	switch runtime.GOOS {
	case "windows", "darwin":
		// Both temp directories are per user (%LOCALAPPDATA%\Temp,
		// /var/folders/…/T) and short enough for sun_path (104–108 bytes).
		return filepath.Join(os.TempDir(), name)
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, name)
	}
	// No per-user runtime dir: /tmp is shared, so the socket goes in a
	// 0700 directory of our own there. If someone else made (or symlinked)
	// that directory, there is no single instance rather than a socket
	// another user could squat on to receive our slack:// links.
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("clack-%d", os.Getuid()))
	if err := privateDir(dir); err != nil {
		log.Printf("desktop: %v; single instance disabled", err)
		return ""
	}
	return filepath.Join(dir, name)
}

// Forward hands req to a running instance and reports whether one took it.
// false: no instance is running (or it didn't answer), so this process
// should start normally.
func Forward(configDir string, req Request) bool {
	return forward(SocketPath(configDir), req)
}

func forward(path string, req Request) bool {
	if path == "" {
		return false
	}
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	allowForeground() // let the running instance raise its window (Windows)
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return false
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	return err == nil && strings.TrimSpace(line) == "ok"
}

// Listen makes this process the primary instance: handle runs (on its own
// goroutine) for every request a later launch forwards. Call Forward first;
// a socket left behind by a crashed instance is replaced.
func Listen(configDir string, handle func(Request)) (*Instance, error) {
	return listen(SocketPath(configDir), handle)
}

func listen(path string, handle func(Request)) (*Instance, error) {
	if path == "" {
		return nil, errors.New("desktop: no private directory for the instance socket")
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		// Stale: nobody answers on it (Forward already failed), so remove
		// it and try once more.
		if c, derr := net.DialTimeout("unix", path, 500*time.Millisecond); derr == nil {
			c.Close()
			return nil, fmt.Errorf("desktop: another instance is listening on %s", path)
		}
		_ = os.Remove(path)
		if ln, err = net.Listen("unix", path); err != nil {
			return nil, err
		}
	}
	_ = os.Chmod(path, 0o600)
	in := &Instance{ln: ln, path: path}
	go in.serve(handle)
	return in, nil
}

func (in *Instance) serve(handle func(Request)) {
	for {
		c, err := in.ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("desktop: instance socket: %v", err)
			}
			return
		}
		go func() {
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			line, err := bufio.NewReader(c).ReadString('\n')
			if err != nil {
				return
			}
			var req Request
			if json.Unmarshal([]byte(line), &req) != nil {
				return
			}
			_, _ = c.Write([]byte("ok\n"))
			handle(req)
		}()
	}
}

// Close stops listening and removes the socket.
func (in *Instance) Close() error {
	if in == nil {
		return nil
	}
	err := in.ln.Close()
	_ = os.Remove(in.path)
	return err
}

// URLArg picks the slack:// link out of command-line arguments (a URL
// handler runs `clack <url>`), or "".
func URLArg(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(a), "slack:") {
			return a
		}
	}
	return ""
}
