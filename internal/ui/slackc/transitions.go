package slackc

import (
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// NativeTransitionsCSS are the CSS transitions Slack's stylesheet declares
// for the parts Clack's natives animate themselves, as written in Slack's
// client-boot-styles (CDN build bv1-13, 2026-11-10) but reduced to their
// transition declarations:
//
//   - the thread pane's read line (ThreadSeparatorRowGeneric with
//     separatorType "read") and its marked-read UnreadDivider dim 3 s
//     after the thread is read (messages/thread_unread.go ghostLine);
//   - ChannelJoinBanner slides down when it's dismissed (ui/joinbanner.go);
//   - agent plan and task_card blocks grow and shrink (blockkit/agent.go).
//
// Everything else Slack transitions (hover colours, menus, toggles…)
// Clack draws in its final state at once, which is what these rules turn
// into under Slick's Snappy. Popover fades and shimmers are keyframe
// animations in Slack, not transitions.
//
// The rules are part of the CSS engine's base sheet, so a native's timing
// follows the cascade (a theme, a plugin or user.css over Slack's rule),
// and Slick installs the same text in the JS page as Slack's linked
// stylesheet (slick.Options.SlackCSS), so plugins that read Slack's CSSOM
// see the rules the natives follow. Snappy finds them there and collapses
// them (transition-duration: .01ms, transition-delay: 0s), and the natives
// change at once.
const NativeTransitionsCSS = `
.p-thread_separator_row_generic.p-thread_separator_row_generic--ghost .p-threads_flexpane__separator_line__read { transition: opacity .1s 3s }
.c-message_list__unread_divider--ghost .c-message_list__unread_divider__separator { transition: opacity .1s 3s }
.channelJoinBannerOuterWrapper__TpBQA { transition: transform .3s, opacity .3s }
.block__G68co { transition: height .25s }
`

// SlackCSSHref is the stylesheet NativeTransitionsCSS comes from, the
// href of the page's <link> for it.
const SlackCSSHref = "https://a.slack-edge.com/bv1-13-br/client-boot-styles.cf726c4f2eaecc74.min.css"

// Transition is the duration and delay of a native's own animation of
// prop ("opacity", "height", "transform"): what CSS gives the widget
// (Slack's rule from NativeTransitionsCSS under any theme, plugin or user
// rule), or def and defDelay when no CSS is active or no transition-*
// declaration reaches the widget. A widget whose transition-property
// leaves prop out (transition: none) changes at once: 0, 0. The delay
// can be negative (the transition starts partway through).
func (s Styled) Transition(prop string, def, defDelay time.Duration) (time.Duration, time.Duration) {
	if s.s == nil || !s.s.trans {
		return def, defDelay
	}
	d, dl, ok := s.s.style.TransitionFor(prop)
	if !ok {
		return 0, 0
	}
	return seconds(d), seconds(dl)
}

// TransitionProgress is how far a transition that started at start has
// got at now (0..1), for a native animating with Transition's dur and
// delay; done is true once it's over (a zero or negative duration is over
// as soon as its delay has passed).
func TransitionProgress(start, now time.Time, dur, delay time.Duration) (p float32, done bool) {
	el := now.Sub(start) - delay
	if el < 0 {
		return 0, false
	}
	if dur <= 0 || el >= dur {
		return 1, true
	}
	return float32(el) / float32(dur), false
}

func seconds(s float32) time.Duration { return time.Duration(float64(s) * float64(time.Second)) }

// hasTransition reports a transition-* declaration in a cascade.
func hasTransition(c *css.Cascaded) bool {
	for i := range c.Decls {
		if strings.HasPrefix(c.Decls[i].Prop, "transition-") {
			return true
		}
	}
	return false
}
