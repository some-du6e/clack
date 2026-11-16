package ui

import (
	"strings"

	"github.com/some-du6e/clack/internal/applog"
)

// The conversation and page view caches (convoFor, App.view) keep the
// views used most recently: every hit moves its key to the back of the
// order, and the front one goes when the cache is full. A channel opened
// early and visited all day stays warm; eviction by creation order threw
// it out first, and reopening it built its list, composer and rows again
// (switches of 302–454 ms into an often visited channel).

const (
	maxConvos = 32 // conversation views (a thread's counts too)
	maxViews  = 24 // page views (App.view)
)

// lruTouch moves k to the back of order (the most recently used end),
// adding it when it's missing.
func lruTouch[K comparable](order []K, k K) []K {
	if n := len(order); n > 0 && order[n-1] == k {
		return order
	}
	for i, x := range order {
		if x == k {
			copy(order[i:], order[i+1:])
			order[len(order)-1] = k
			return order
		}
	}
	return append(order, k)
}

// noteEvicted logs a view the cache let go (debug log).
func noteEvicted(cache, key string) {
	applog.Debugf("ui: %s cache evicted %s", cache, strings.TrimSuffix(key, "/"))
}
