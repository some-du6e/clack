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
	"sync/atomic"
	"time"
)

// Request is what a second launch hands the running instance.
type Request struct {
	// URL is a slack:// link to open ("" just focuses the window).
	URL string `json:"url,omitempty"`
}

// ErrAnotherInstance is Listen's error when another Clack listens on the
// socket (it didn't take Forward's request in time): this process isn't
// the primary instance.
var ErrAnotherInstance = errors.New("desktop: another instance is listening")

// Instance is the primary (first) Clack process's listener.
type Instance struct {
	ln   net.Listener
	path string
	// quitting: Run returned (Quitting); launches are told to wait
	quitting atomic.Bool
}

// InstanceKey names configDir's instance: separate CLACK_HOME directories
// are separate instances (their own socket, their own run record).
func InstanceKey(configDir string) string {
	abs, err := filepath.Abs(configDir)
	if err != nil {
		abs = configDir
	}
	h := sha256.Sum256([]byte(abs))
	return fmt.Sprintf("%x", h[:6])
}

// SocketPath is the per-user, per-config-directory socket the instances
// talk over (AF_UNIX works on Linux, macOS and Windows 10 1803+). Separate
// CLACK_HOME directories get separate instances.
func SocketPath(configDir string) string {
	name := "clack-" + InstanceKey(configDir) + ".sock"
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
	deadline := time.Now().Add(forwardQuitWait)
	for {
		switch forwardOnce(path, req) {
		case "ok":
			return true
		case quittingReply:
			// the running Clack is quitting: once its socket goes this
			// launch is the Clack (Listen); a quit that takes longer
			// than forwardQuitWait leaves it to Listen's own answer
			if time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		return false
	}
}

// forwardQuitWait is how long a launch waits for a quitting Clack to go.
var forwardQuitWait = 30 * time.Second

// forwardOnce hands req to the instance on path once: its answer, or ""
// when nobody answered.
func forwardOnce(path string, req Request) string {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return ""
	}
	defer c.Close()
	allowForeground() // let the running instance raise its window (Windows)
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return ""
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
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
			return nil, fmt.Errorf("%w on %s", ErrAnotherInstance, path)
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
			if in.quitting.Load() {
				// quitting (Run returned): a launch now waits for the
				// socket to go and starts on its own, rather than
				// handing over to a Clack that won't show it
				_, _ = c.Write([]byte(quittingReply + "\n"))
				return
			}
			_, _ = c.Write([]byte("ok\n"))
			handle(req)
		}()
	}
}

// quittingReply is a quitting instance's answer to a forwarded launch.
const quittingReply = "quitting"

// Quitting marks the instance as quitting: from now on a later launch is
// told so, waits for this one to close the socket, and starts on its own
// (an update installing on quit, the run record and the logs keep a
// quitting Clack up for seconds after its window went).
func (in *Instance) Quitting() {
	if in != nil {
		in.quitting.Store(true)
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
