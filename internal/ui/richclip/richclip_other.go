//go:build !windows && !(darwin && !ios && cgo)

package richclip

import "context"

func supported() bool { return false }

func write(context.Context, Copy) error { return ErrUnsupported }

func readCustom(context.Context) (map[string]string, error) { return nil, ErrUnsupported }
