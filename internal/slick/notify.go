package slick

import (
	"context"
	"log"
	"time"
)

// NotificationOptions is a toast as Slick's notification filters see it:
// Electron's NotificationConstructorOptions, the notification Slack's
// renderer made and its main process is about to show.
type NotificationOptions struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body"`
	Silent   bool   `json:"silent"`
}

// AllowNotification runs the main halves' notifications.filter chain over
// a toast about to show, as Slick's Notification.prototype.show patch
// does (src/desktop/pluginHost.ts): false when a filter doesn't permit it.
// With no filter, or no runtime, it permits with no JS call. A filter that
// throws permits, as Slick's does; so does a chain that can't answer in
// time (a stuck runtime doesn't lose the toast). Any goroutine but the JS
// thread.
func (s *Slick) AllowNotification(ctx context.Context, o NotificationOptions) bool {
	if s == nil || s.noteFilters.Load() <= 0 {
		return true
	}
	s.mu.Lock()
	rt := s.rt
	s.mu.Unlock()
	if rt == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ok := true
	if err := s.call(ctx, rt, "allowNotification", &ok, o); err != nil {
		log.Printf("slick: notification filters: %v", err)
		return true
	}
	return ok
}
