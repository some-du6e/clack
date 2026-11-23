package ui

import "github.com/some-du6e/clack/internal/jsrt"

// pickFilesHook, when set (tests), is the chooser a plugin's file input
// opens (App.pickFiles), in place of the platform's.
var pickFilesHook func(accept string, multiple bool) ([]jsrt.PickedFile, error)
