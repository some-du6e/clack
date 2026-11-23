package ui

import "github.com/some-du6e/clack/internal/jsrt"

// pickFilesHook, when set (tests), is every chooser App.pickFiles opens,
// in place of the platform's: a plugin's file input on the page, Block
// Kit's file input, and the Taut, Slick and Plugins import choosers.
var pickFilesHook func(accept string, multiple bool) ([]jsrt.PickedFile, error)
