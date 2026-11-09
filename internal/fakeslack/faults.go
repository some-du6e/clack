package fakeslack

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Fault makes calls to one method misbehave (fault-injection tests for
// the upload path; nothing here is Slack's behaviour, only the network's
// and the server's failures).
type Fault struct {
	Delay  time.Duration // before answering (a slow upload)
	Status int           // answer with this HTTP status and no body
	Error  string        // answer {"ok": false, "error": Error}
	HangUp bool          // drop the connection without an answer
	// AfterHandle applies Status/Error/HangUp after the call took effect
	// (a share that happened but whose reply was lost).
	AfterHandle bool
	// Times is how many calls it hits (0: every call).
	Times int
}

// SetFault injects f into method's calls ("files.getUploadURL",
// "upload" for the byte POSTs, "files.completeUpload", "files.share").
// A zero Fault clears it.
func (s *Server) SetFault(method string, f Fault) {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	if s.faults == nil {
		s.faults = map[string]*Fault{}
	}
	if f == (Fault{}) {
		delete(s.faults, method)
		return
	}
	s.faults[method] = &f
}

func (s *Server) takeFault(method string) (Fault, bool) {
	if strings.HasPrefix(method, "upload/") {
		method = "upload"
	}
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	f := s.faults[method]
	if f == nil {
		return Fault{}, false
	}
	out := *f
	if f.Times > 0 {
		if f.Times--; f.Times == 0 {
			delete(s.faults, method)
		}
	}
	return out, true
}

// applyFault answers with the fault; false when it only delays.
func (s *Server) applyFault(w http.ResponseWriter, f Fault) bool {
	switch {
	case f.HangUp:
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				c.Close()
				return true
			}
		}
		w.WriteHeader(http.StatusBadGateway)
		return true
	case f.Status != 0:
		w.WriteHeader(f.Status)
		return true
	case f.Error != "":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": f.Error})
		return true
	}
	return false
}
