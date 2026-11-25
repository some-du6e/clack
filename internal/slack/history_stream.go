package slack

import (
	"bytes"
	"encoding/json"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// A page of #giant-wall's shape (28 messages of 908,000 characters, their
// text twice: the text field and the rich_text block) is 50 MB of JSON.
// json.Unmarshal took 1.9 s over it on a CI runner: checkValid scans the
// whole body first, then the decode scans it again, at about 50 MB/s each
// (TestGiantWallOpenSpans' CPU profile). The page's messages are
// independent, so readStreamed hands each one to json.Unmarshal on its
// own, on every core, as soon as its last byte has come, while the rest of
// the body is still on the wire. The answer around them (ok, has_more,
// pin_count, response_metadata, ...) is decoded by json.Unmarshal too,
// with the messages array emptied. Every value comes from json.Unmarshal
// of the bytes it was decoded from in the whole body, so the page is the
// one json.Unmarshal would make; anything the split can't vouch for (a
// body that isn't one object, a second key encoding/json would also put
// in Messages, an element that isn't an object, a separator out of place,
// an element past maxStreamedElement, an element json.Unmarshal refuses)
// gives the whole body back for the old decode, which then fails or
// succeeds exactly as it did; so do an element nested deeper than
// json.Unmarshal takes in the whole body, and more elements than any
// page Clack asks for. The body is kept in the chunks it came in, never
// joined into a second copy (readBody joins a gzipped answer's chunks:
// 100 MB for a 50 MB page) unless the old decode needs it.

// maxStreamedElement bounds one message's bytes: a larger one is copied
// out of no chunk, and the whole body goes the old way instead.
const maxStreamedElement = 64 << 20

// maxStreamedElements bounds a page's messages, far above any limit Clack
// asks (28 a page, 200 a preview): an array that goes on past it (an
// answer that never ends) goes the old way, whose memory holds only its
// bytes, not a decoded message for every 3 of them.
const maxStreamedElements = 1000

// maxElementDepth is the deepest an element may nest (its own braces
// counted from 1): encoding/json refuses a document nested past 10000
// (maxNestingDepth), and an element sits under the answer's object and
// its messages array, so json.Unmarshal of the whole body refuses one
// past 9998 that json.Unmarshal of the element alone would take.
const maxElementDepth = 10000 - 2

// inlineElement: an element shorter than this is decoded on the reading
// goroutine as it ends (a few microseconds), a longer one on a worker.
const inlineElement = 64 << 10

// streamChunks are the sizes a body of unknown length is read in: the
// first, doubling to the last, as readBody's (a gzipped answer the
// transport decompresses has no length: every production answer). A
// small answer takes a small chunk, and a giant one's messages (about
// 1.8 MB each on #giant-wall) mostly fit one. Tests set them small, so
// chunk boundaries fall everywhere.
var streamChunks = [2]int{8 << 10, 4 << 20}

// decodeSlots bounds the workers decoding elements at once, across every
// call: one core stays free for the UI (clamp(GOMAXPROCS-1, 1, 4)). A
// call that finds no slot free and has no worker decodes on its reading
// goroutine.
var decodeSlots = make(chan struct{}, max(1, min(runtime.GOMAXPROCS(0)-1, 4)))

// readStreamed reads the answer's body from r (hint: its Content-Length,
// -1 when unknown). done: p holds what Call's decode of the whole body
// would have put in it. Otherwise body is the whole body, for Call to take
// as it takes every other (its ok, its error, json.Unmarshal), p
// untouched; err is a read's error (body nil: Call drops what came).
// Call reads only a *HistoryPage this way: a type embedding it would lose
// its other fields, so Call asks for the type itself.
func (p *HistoryPage) readStreamed(r io.Reader, hint int64) (done bool, body []byte, err error) {
	var page HistoryPage
	s := newMsgSplitter(func(elem []byte, i int, dst func(int, Message, error)) {
		var m Message
		err := json.Unmarshal(elem, &m)
		dst(i, m, err)
	})
	got, err := s.read(r, hint)
	read := time.Now()
	msgs, ok := s.finish()
	if err != nil {
		return false, nil, err
	}
	if !ok {
		return false, got.join(), nil
	}
	sk := s.skeleton()
	if !okFast(sk) { // Call's own head check: its ok, an error of the wrong type
		var head struct {
			OK    *bool  `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(sk, &head); err != nil || head.OK != nil && !*head.OK {
			return false, got.join(), nil // Call's own: its error, from the whole body
		}
	}
	if err := json.Unmarshal(sk, &page); err != nil {
		return false, got.join(), nil
	}
	if s.arrayEnd > 0 { // the messages array, decoded element by element
		page.Messages = msgs
	}
	*p = page
	if f := CallTimes.Load(); f != nil {
		n := 0
		for _, c := range got {
			n += len(c)
		}
		(*f)("conversations.history", n, read, time.Now())
	}
	return true, nil, nil
}

// chunks is a body as it came in.
type chunks [][]byte

// join is the body in one slice, as readBody returns it: a single chunk
// mostly empty is copied out (an error answer's body is kept, Error.Body
// in the client's backoff guard, for as long as the request fails), and
// no chunk at all is an empty body.
func (c chunks) join() []byte {
	switch len(c) {
	case 0:
		return []byte{}
	case 1:
		if len(c[0]) >= cap(c[0])/2 {
			return c[0]
		}
		return bytes.Clone(c[0])
	}
	return bytes.Join(c, nil)
}

// msgSplitter finds the elements of a JSON object's top-level "messages"
// array as its bytes come in, and decodes each on a worker.
type msgSplitter struct {
	decode func(elem []byte, i int, dst func(int, Message, error))

	// the scan, across chunks
	off      int64 // bytes seen
	phase    int   // spBefore, spObject, spArray, spAfter, spDone, spBad
	depth    int   // nesting below the top-level object (phase spObject/spAfter) or in an element (spArray)
	inStr    bool  // inside a string
	esc      bool  // after a backslash in a string
	key      []byte
	keyDone  bool // the string just read was a key (before its colon)
	wantKey  bool // the next string at depth 0 of the object is a key
	inKey    bool // reading a top-level key's string
	keyBad   bool // the key had an escape (it can't be compared)
	afterKey int  // 0, 1 after a "messages" key (waiting for its colon), 2 after its colon (waiting for the array)
	seenMsgs bool

	// the array
	arrayStart, arrayEnd int64 // offsets of its '[' and ']'
	inElem               bool
	elemStart            int64
	elem                 []byte // the element so far, when it spans chunks
	elemChunkStart       int    // its start in the current chunk, -1 when it began in an earlier one
	expectSep            bool   // after an element: a ',' or the ']' next
	n                    int    // elements started

	// what is kept for the skeleton: the bytes before the array and after it
	prefix, suffix []byte

	// the decoding: the call's workers (each holding one of decodeSlots)
	// and what they decoded. failed: an element json.Unmarshal refused (no
	// more are handed out: the page goes the old way); stop: the read
	// failed, and what's queued isn't decoded.
	work    chan elemJob
	workers int
	wg      sync.WaitGroup
	failed  atomic.Bool
	stop    atomic.Bool
	mu      sync.Mutex
	results []Message
}

type elemJob struct {
	b []byte
	i int
}

const (
	spBefore = iota
	spObject
	spArray
	spAfter
	spDone
	spBad
)

// maxSkeleton bounds what is kept of the answer around the messages array.
const maxSkeleton = 4 << 20

func newMsgSplitter(decode func([]byte, int, func(int, Message, error))) *msgSplitter {
	return &msgSplitter{decode: decode, phase: spBefore, elemChunkStart: -1}
}

// dispatch decodes an element: a short one here, a long one on a worker,
// started (one per slot, up to decodeSlots' size) as the long ones come.
// With no slot free and no worker of its own the call decodes it here.
func (s *msgSplitter) dispatch(j elemJob) {
	if len(j.b) < inlineElement {
		s.decode(j.b, j.i, s.store)
		return
	}
	if s.workers < cap(decodeSlots) {
		select {
		case decodeSlots <- struct{}{}:
			if s.work == nil {
				s.work = make(chan elemJob, cap(decodeSlots))
			}
			s.workers++
			s.wg.Add(1)
			go func() {
				defer func() { <-decodeSlots; s.wg.Done() }()
				for j := range s.work {
					if !s.stop.Load() {
						s.decode(j.b, j.i, s.store)
					}
				}
			}()
		default:
		}
	}
	if s.workers == 0 {
		s.decode(j.b, j.i, s.store)
		return
	}
	s.work <- j
}

func (s *msgSplitter) store(i int, m Message, err error) {
	if err != nil {
		s.failed.Store(true)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.results) <= i {
		s.results = append(s.results, Message{})
	}
	s.results[i] = m
}

// read reads the body, scanning each chunk as it comes. A read's error
// (a cut connection, a cancelled context, a gzip stream cut short) stops
// the decoding of what's queued and comes back with no body.
func (s *msgSplitter) read(r io.Reader, hint int64) (chunks, error) {
	size, grow := streamChunks[0], true
	if hint > 0 && hint < 1<<30 {
		size, grow = int(hint)+1, false // the byte more sees the end without a second chunk
	}
	var body chunks
	c := make([]byte, 0, size)
	scanned := 0
	for {
		n, err := r.Read(c[len(c):cap(c)])
		c = c[:len(c)+n]
		if n > 0 && s.phase != spBad {
			s.scan(c, scanned)
			scanned = len(c)
		}
		if err != nil {
			if err != io.EOF {
				s.stop.Store(true)
				s.bad()
				return nil, err
			}
			if len(c) > 0 {
				body = append(body, c)
			}
			return body, nil
		}
		if len(c) == cap(c) {
			switch {
			case !grow:
				size = streamChunks[1] // longer than its Content-Length said
			case size < streamChunks[1]:
				size = min(size*2, streamChunks[1])
			}
			s.endChunk(c, size)
			body = append(body, c)
			c = make([]byte, 0, size)
			scanned = 0
		}
	}
}

// endChunk carries an element in progress over to the next chunk, next
// bytes long, into a copy with room for it: the copy grows a chunk at a
// time, not by append's quarters.
func (s *msgSplitter) endChunk(c []byte, next int) {
	if s.phase != spArray || !s.inElem {
		return
	}
	part := c
	if s.elemChunkStart >= 0 {
		part = c[s.elemChunkStart:]
		s.elem = nil
	}
	if need := len(s.elem) + len(part); cap(s.elem) < need {
		e := make([]byte, len(s.elem), need+next)
		copy(e, s.elem)
		s.elem = e
	}
	s.elem = append(s.elem, part...)
	s.elemChunkStart = -1
	if len(s.elem) > maxStreamedElement {
		s.bad()
	}
}

// bad gives the body up to the old decode; the element in progress goes.
func (s *msgSplitter) bad() { s.phase, s.elem = spBad, nil }

// scan reads c[from:], the new bytes of the current chunk.
func (s *msgSplitter) scan(c []byte, from int) {
	for i := from; i < len(c); i++ {
		b := c[i]
		pos := s.off + int64(i-from)
		switch s.phase {
		case spBefore:
			s.keep(b)
			switch b {
			case ' ', '\t', '\n', '\r':
			case '{':
				s.phase, s.wantKey = spObject, true
			default:
				s.bad()
				return
			}
		case spObject, spAfter:
			if s.phase == spObject && s.afterKey == 2 {
				// the "messages" value
				switch b {
				case ' ', '\t', '\n', '\r':
					s.keep(b)
				case '[':
					s.keep(b)
					s.phase, s.arrayStart, s.afterKey = spArray, pos, 0
					s.seenMsgs = true
				default:
					s.bad()
					return
				}
				continue
			}
			s.keep(b)
			if s.inStr {
				switch {
				case s.esc:
					s.esc = false
					if s.inKey {
						s.keyBad = true
					}
				case b == '\\':
					s.esc = true
					if s.inKey {
						s.keyBad = true
					}
				case b == '"':
					s.inStr = false
					if s.inKey {
						s.inKey = false
						s.keyDone = true
					}
				default:
					if s.inKey {
						if len(s.key) < 16 {
							s.key = append(s.key, b)
						} else {
							s.key = append(s.key[:16], 'x') // longer than "messages": no match
						}
					}
				}
				continue
			}
			switch b {
			case '"':
				s.inStr = true
				if s.depth == 0 && s.wantKey {
					s.inKey, s.wantKey, s.keyBad, s.key = true, false, false, s.key[:0]
				}
			case ':':
				if s.depth == 0 && s.keyDone {
					s.keyDone = false
					if s.keyBad {
						s.bad() // escaped: can't tell whether encoding/json puts it in Messages
						return
					}
					if bytes.EqualFold(s.key, []byte("messages")) {
						if s.seenMsgs || !bytes.Equal(s.key, []byte("messages")) {
							s.bad() // a second (or folded) key for Messages
							return
						}
						s.afterKey = 2
					}
				}
			case '{', '[':
				s.depth++
			case '}', ']':
				if s.depth == 0 {
					if b == '}' {
						s.phase = spDone
						s.wantKey = false
					} else {
						s.bad()
						return
					}
				} else {
					s.depth--
				}
			case ',':
				if s.depth == 0 {
					s.wantKey = true
				}
			}
		case spArray:
			if s.inElem {
				if s.inStr {
					st := i
					if s.esc { // a backslash ending the last chunk escapes this byte
						s.esc = false
						st = i + 1
					}
					q := quoteEnd(c, st)
					if q < 0 {
						s.esc = oddBackslashes(c, st)
						i = len(c) - 1
						continue
					}
					i, s.inStr = q, false
					continue
				}
				switch b {
				case '"':
					s.inStr = true
				case '{', '[':
					s.depth++
					if s.depth > maxElementDepth {
						s.bad() // json.Unmarshal refuses the whole body
						return
					}
				case '}', ']':
					s.depth--
					if s.depth == 0 {
						s.endElem(c, i, pos)
						if s.phase == spBad {
							return
						}
					}
				}
				continue
			}
			switch b {
			case ' ', '\t', '\n', '\r':
			case ',':
				if !s.expectSep {
					s.bad()
					return
				}
				s.expectSep = false
			case ']':
				if s.n > 0 && !s.expectSep { // "[x,]"
					s.bad()
					return
				}
				s.phase, s.arrayEnd = spAfter, pos
				s.keep(']')
				s.expectSep = false
			case '{':
				if s.expectSep {
					s.bad()
					return
				}
				s.inElem, s.depth, s.elemStart, s.elemChunkStart = true, 1, pos, i
				s.elem = s.elem[:0]
			default: // a scalar, a string or an array as an element: the old way
				s.bad()
				return
			}
		case spDone:
			s.keep(b)
			switch b {
			case ' ', '\t', '\n', '\r':
			default:
				s.bad() // trailing bytes: json.Unmarshal's error
				return
			}
		}
		if s.phase == spBad {
			return
		}
	}
	s.off += int64(len(c) - from)
}

// endElem decodes the element that ended at c[i] (dispatch), unless an
// earlier one failed or the page has more than any Clack asks for.
func (s *msgSplitter) endElem(c []byte, i int, pos int64) {
	var b []byte
	if s.elemChunkStart >= 0 {
		b = c[s.elemChunkStart : i+1] // the chunk is never written again
	} else {
		b = append(s.elem, c[:i+1]...)
		s.elem = nil
	}
	if int64(len(b)) != pos-s.elemStart+1 || len(b) > maxStreamedElement || s.failed.Load() || s.n >= maxStreamedElements {
		s.bad()
		return
	}
	s.inElem, s.elemChunkStart, s.expectSep = false, -1, true
	s.dispatch(elemJob{b, s.n})
	s.n++
}

// quoteEnd is the index in c of the first quote at or after from that no
// backslash escapes (an odd run of them right before it), or -1. A run
// before from doesn't count: c[from-1] is the string's opening quote or a
// byte a backslash escaped.
func quoteEnd(c []byte, from int) int {
	for i := from; i <= len(c); {
		j := bytes.IndexByte(c[i:], '"')
		if j < 0 {
			return -1
		}
		q := i + j
		k := q
		for k > from && c[k-1] == '\\' {
			k--
		}
		if (q-k)%2 == 0 {
			return q
		}
		i = q + 1
	}
	return -1
}

// oddBackslashes reports whether c[from:] ends in an odd run of
// backslashes, the last of which escapes the next chunk's first byte.
func oddBackslashes(c []byte, from int) bool {
	k := len(c)
	for k > from && c[k-1] == '\\' {
		k--
	}
	return (len(c)-k)%2 == 1
}

// keep keeps a byte of the answer around the array for the skeleton.
func (s *msgSplitter) keep(b byte) {
	if s.phase == spArray {
		return
	}
	dst := &s.prefix
	if s.seenMsgs {
		dst = &s.suffix
	}
	if len(*dst) >= maxSkeleton {
		s.bad()
		return
	}
	*dst = append(*dst, b)
}

// finish waits for the workers, and reports the messages and whether the
// split vouches for them.
func (s *msgSplitter) finish() ([]Message, bool) {
	if s.work != nil {
		close(s.work)
	}
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != spDone || s.failed.Load() || s.inStr {
		return nil, false
	}
	msgs := s.results
	if msgs == nil {
		msgs = []Message{}
	}
	if len(msgs) != s.n {
		return nil, false
	}
	return msgs, true
}

// skeleton is the answer with its messages array empty: the bytes up to
// and with its '[', then from its ']' on.
func (s *msgSplitter) skeleton() []byte {
	out := make([]byte, 0, len(s.prefix)+len(s.suffix))
	out = append(out, s.prefix...)
	return append(out, s.suffix...)
}
