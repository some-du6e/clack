package fakeslack

// Big files: a generated body of any size behind /files-pri/, for the
// download tests (core's streaming download). Nothing is stored: byte i of
// a big file is BigFileByte(id, i), so a test checks a download without
// holding either copy. Ranges (206), an ETag and a Content-Disposition
// name are answered as files.slack.com does; a file can be cut off
// mid-body or held at a byte until released.

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// BigFile is how a big file is served.
type BigFile struct {
	Name string
	Size int64
	// Cut ends that many answers (0: none) after CutAt bytes of the file
	// have gone (the connection closed under a full Content-Length).
	Cut   int
	CutAt int64
	// HoldAt stops the body at that byte (0: never) until ReleaseBigFile.
	HoldAt int64
	// NoRanges answers every request with the whole file (200).
	NoRanges bool
}

type bigFiles struct {
	mu    sync.Mutex
	files map[string]*bigFileState
}

type bigFileState struct {
	f        BigFile
	release  chan struct{}
	served   int // requests answered
	ranges   []string
	released bool
}

var bigFileStores = newPerServer() // *Server -> *bigFiles

func (s *Server) bigFiles() *bigFiles {
	v, _ := bigFileStores.LoadOrStore(s, &bigFiles{files: map[string]*bigFileState{}})
	return v.(*bigFiles)
}

// AddBigFile serves file id as f and returns its slack.File (hosted, a
// Windows installer's mimetype unless the name says otherwise).
func (s *Server) AddBigFile(id string, f BigFile) slack.File {
	b := s.bigFiles()
	b.mu.Lock()
	b.files[id] = &bigFileState{f: f, release: make(chan struct{})}
	b.mu.Unlock()
	return slack.File{
		ID: id, Name: f.Name, Title: f.Name, Mimetype: "application/x-msdownload", Filetype: "exe", PrettyType: "Windows Application",
		Size: f.Size, Mode: "hosted",
		URLPrivate:         s.URL + "files-pri/" + TeamID + "-" + id + "/" + f.Name,
		URLPrivateDownload: s.URL + "files-pri/" + TeamID + "-" + id + "/download/" + f.Name,
		Permalink:          s.URL + "files/" + SelfID + "/" + id + "/" + f.Name,
	}
}

// ReleaseBigFile lets a held body go on.
func (s *Server) ReleaseBigFile(id string) {
	b := s.bigFiles()
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.files[id]; st != nil && !st.released {
		st.released = true
		close(st.release)
	}
}

// BigFileRequests is how many requests file id was answered, and their
// Range headers.
func (s *Server) BigFileRequests(id string) (int, []string) {
	b := s.bigFiles()
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.files[id]; st != nil {
		return st.served, append([]string(nil), st.ranges...)
	}
	return 0, nil
}

// BigFileByte is byte off of big file id: a stream no shorter period than
// the file repeats (an xorshift of the offset, seeded by the id).
func BigFileByte(id string, off int64) byte {
	var p [1]byte
	BigFileFill(id, off, p[:])
	return p[0]
}

// BigFileFill puts bytes off.. of big file id in p.
func BigFileFill(id string, off int64, p []byte) {
	h := fnv.New64a()
	h.Write([]byte(id))
	seed := h.Sum64()
	for i := range p {
		x := uint64(off+int64(i))*0x9E3779B97F4A7C15 ^ seed
		x ^= x >> 31
		x *= 0xBF58476D1CE4E5B9
		x ^= x >> 29
		p[i] = byte(x)
	}
}

// serveBigFile answers a big file's GET (filesPri).
func (s *Server) serveBigFile(w http.ResponseWriter, r *http.Request, id string) bool {
	b := s.bigFiles()
	b.mu.Lock()
	st := b.files[id]
	if st == nil {
		b.mu.Unlock()
		return false
	}
	f := st.f
	st.served++
	st.ranges = append(st.ranges, r.Header.Get("Range"))
	cut := st.served <= f.Cut
	release := st.release
	b.mu.Unlock()

	start, end := int64(0), f.Size-1
	status := http.StatusOK
	if rg := r.Header.Get("Range"); rg != "" && !f.NoRanges {
		a, z, ok := parseBytesRange(rg, f.Size)
		if !ok {
			w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(f.Size, 10))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return true
		}
		start, end, status = a, z, http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, f.Size))
	}
	w.Header().Set("Content-Type", "application/x-msdownload")
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	w.Header().Set("ETag", `"`+id+`-v1"`)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(status)
	buf := make([]byte, 64<<10)
	for off := start; off <= end; {
		n := int64(len(buf))
		if rest := end - off + 1; rest < n {
			n = rest
		}
		if f.HoldAt > 0 && off < f.HoldAt && off+n > f.HoldAt {
			n = f.HoldAt - off
		}
		if cut && off < f.CutAt && off+n > f.CutAt {
			n = f.CutAt - off
		}
		if f.HoldAt > 0 && off == f.HoldAt {
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		if cut && off == f.CutAt {
			// cut off: the connection goes under a Content-Length not met
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					c.Close()
				}
			}
			return true
		}
		BigFileFill(id, off, buf[:n])
		if _, err := w.Write(buf[:n]); err != nil {
			return true
		}
		off += n
	}
	return true
}

// parseBytesRange reads "bytes=a-" or "bytes=a-b" against size.
func parseBytesRange(v string, size int64) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(v), "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	a, z, found := strings.Cut(spec, "-")
	if !found || a == "" {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(a, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end = size - 1
	if z != "" {
		e, err := strconv.ParseInt(z, 10, 64)
		if err != nil || e < start {
			return 0, 0, false
		}
		if e < end {
			end = e
		}
	}
	return start, end, true
}
