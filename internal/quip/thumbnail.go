package quip

import (
	"errors"
	"strconv"
)

// A canvas's Block Kit thumbnail, as Slack's message cards read it
// (useQuipBlocks, boot-shared 6WLC: the Quip thread's
// getBlockKitThumbnailValue, which the collab webapp fills from
// threadBlockKitThumbnail, WpDR; build 8c42ff1ca93bfb04): load the
// thread object, then its BLOCK_KIT_THUMBNAIL parcel keyed by the
// thread's document id at the thread's updated_usec. The parcel's data
// is the thumbnail's JSON, {"blocks": [...]}.

// ErrThumbnail is a thumbnail parcel that failed to load (WpDR answers
// `{"blocks": [], "error": true}`, and useQuipBlocks draws no preview).
var ErrThumbnail = errors.New("quip: the thumbnail parcel didn't load")

// NewThreadRequest is load-data's request for a thread object alone, as
// the syncer's loadObject asks for one (object_ids, nothing else): what
// threadBlockKitThumbnailImpl loads first.
func NewThreadRequest(thread string) *LoadRequest {
	return &LoadRequest{ObjectIDs: []string{thread}}
}

// ReadThread is the thread object of a NewThreadRequest answer.
func ReadThread(thread string, w *LoadWrapper) (*Thread, error) {
	r, err := loadResponse(w)
	if err != nil {
		return nil, err
	}
	if r.Objects != nil {
		for i := range r.Objects.Threads {
			if t := &r.Objects.Threads[i]; str(t.ID) == thread {
				return t, nil
			}
		}
	}
	return nil, &LoadError{Message: "no thread " + thread + " in the answer"}
}

// NewThumbnailRequest is load-data's request for the thread's Block Kit
// thumbnail (threadBlockKitThumbnailImpl's Parcel.get: type
// BLOCK_KIT_THUMBNAIL, the document id as its key, the thread's
// updated_usec as its version, the thread id; no hash for a parcel the
// syncer doesn't hold).
func NewThumbnailRequest(thread, document string, updatedUsec int64) *LoadRequest {
	return &LoadRequest{Parcels: []Parcel{{Type: Ptr(ParcelBlockKitThumbnail), Key: Ptr(document),
		Version: Ptr(strconv.FormatInt(updatedUsec, 10)), ThreadID: Ptr(thread)}}}
}

// ReadThumbnail is a NewThumbnailRequest answer's thumbnail JSON for
// document: the parcel's data, `{"blocks": []}` when it has none
// (threadBlockKitThumbnailImpl), ErrThumbnail when the answer failed or
// lacks the parcel (HVRg sets a load error on a parcel the answer leaves
// out: setLoadErrorForMissingItems).
func ReadThumbnail(document string, w *LoadWrapper) ([]byte, error) {
	r, err := loadResponse(w)
	if err != nil {
		return nil, ErrThumbnail
	}
	for _, p := range r.Parcels {
		if p.Type != nil && *p.Type == ParcelBlockKitThumbnail && str(p.Key) == document {
			if p.Data == nil {
				return []byte(`{"blocks": []}`), nil
			}
			return p.Data, nil
		}
	}
	return nil, ErrThumbnail
}

// loadResponse is w's response, or its error (ReadLoad's: the wrapper's
// error, a missing response, the failures).
func loadResponse(w *LoadWrapper) (*LoadResponse, error) {
	if w == nil {
		return nil, ErrNotFound
	}
	if w.Error != nil {
		return nil, &LoadError{Message: *w.Error}
	}
	if w.Response == nil {
		return nil, ErrNotFound
	}
	if len(w.Response.Failures) > 0 {
		e := &LoadError{}
		for _, f := range w.Response.Failures {
			if f.Reason != nil {
				e.Reasons = append(e.Reasons, *f.Reason)
			} else {
				e.Reasons = append(e.Reasons, FailAuth)
			}
		}
		return nil, e
	}
	return w.Response, nil
}
