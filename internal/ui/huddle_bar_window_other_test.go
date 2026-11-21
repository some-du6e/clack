//go:build !windows

package ui

import "testing"

const canCloseHuddleWindowLikeYou = false

func closeHuddleWindowLikeYou(t *testing.T) { t.Helper() }
