package messages

import "time"

// pageLimiter is the message pane's HistoryRateLimiter (boot-shared WWGY,
// build e277bbacc0bc), which BaseMessagePane.requestHistory asks before
// every page it requests: a request key (`${oldest}_${latest}`) goes at
// most twice in 10 s, and a key that keeps reaching the limit waits twice
// as long each window, up to 160 s. Nothing else spaces the pages:
// MessageList.requestOlder asks again as soon as the last one landed (it
// refuses only while one is out), so a reader paging back gets a page per
// round trip, and only a page asked for again (a failed one, or one that
// brought nothing new) is held. The pane makes one limiter and resets it
// on a channel change (componentDidUpdate); a list here is one
// conversation's, and an open starts it afresh (NewChannelViewIn).
type pageLimiter struct {
	reqs map[string]*pageReq
}

// pageReq is a key's window: the requests in it, when it began, how long
// it lasts.
type pageReq struct {
	count    int
	time     time.Time
	duration time.Duration
}

const (
	pageReqLimit    = 2                 // HistoryRateLimiter's limit
	pageReqDuration = 10 * time.Second  // its duration
	pageReqMax      = 160 * time.Second // the longest a window grows to (16e4)
)

// log is logHistoryRequest: a request for key went out at now. A window
// that ended starts again with this request, twice as long when the last
// one reached the limit, else the first duration again.
func (l *pageLimiter) log(key string, now time.Time) {
	if l.reqs == nil {
		l.reqs = map[string]*pageReq{}
	}
	r := l.reqs[key]
	if r == nil {
		r = &pageReq{duration: pageReqDuration}
		l.reqs[key] = r
	}
	if now.Sub(r.time) > r.duration { // a - r.duration > r.time
		if r.count >= pageReqLimit {
			r.duration = min(2*r.duration, pageReqMax)
		} else {
			r.duration = pageReqDuration
		}
		r.count, r.time = 1, now
	} else {
		r.count++
	}
}

// prevent is shouldPreventHistoryRequest: key reached the limit in a
// window not over yet.
func (l *pageLimiter) prevent(key string, now time.Time) bool {
	r := l.reqs[key]
	return r != nil && r.count >= pageReqLimit && !r.time.Before(now.Add(-r.duration))
}

// reset is the pane's historyRateLimiter.reset().
func (l *pageLimiter) reset() { l.reqs = nil }
