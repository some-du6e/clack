//go:build linux && !android

package composer

import (
	"context"
	"os"
	"os/exec"
)

// Linux clipboard images and file pickers, via command-line tools when
// installed: wl-paste (wl-clipboard) on Wayland, xclip on X11/XWayland;
// zenity or kdialog for the chooser.

func attClipboardSupported() bool { return true }

// attClipboardImage reads an image from the system clipboard. It returns
// errAttNoClipImage when the clipboard holds none and errAttClipTools when
// no clipboard tool is installed.
func attClipboardImage(ctx context.Context, explicit bool) ([]byte, string, error) {
	wayland := os.Getenv("WAYLAND_DISPLAY") != ""
	x11 := os.Getenv("DISPLAY") != ""
	switch {
	case wayland && attHave("wl-paste"):
		return attClipVia(ctx, explicit,
			[]string{"wl-paste", "--list-types"},
			func(t string) []string { return []string{"wl-paste", "--no-newline", "--type", t} })
	case x11 && attHave("xclip"):
		return attClipVia(ctx, explicit,
			[]string{"xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"},
			func(t string) []string { return []string{"xclip", "-selection", "clipboard", "-t", t, "-o"} })
	}
	return nil, "", errAttClipTools
}

func attClipVia(ctx context.Context, explicit bool, list []string, get func(typ string) []string) ([]byte, string, error) {
	out, err := attRun(ctx, 1<<20, list[0], list[1:]...)
	if err != nil {
		// Both tools exit non-zero when the clipboard is empty.
		return nil, "", errAttNoClipImage
	}
	typ := attPickImageType(attParseTargets(string(out)), explicit)
	if typ == "" {
		return nil, "", errAttNoClipImage
	}
	args := get(typ)
	data, err := attRun(ctx, attMaxFileSize, args[0], args[1:]...)
	if err == errAttTooBig {
		return nil, "", err
	}
	if err != nil || len(data) == 0 {
		return nil, "", errAttNoClipImage
	}
	return data, typ, nil
}

// attNativePick runs zenity or kdialog. ok is false when neither is
// installed (or there's no display); paths is nil when the user cancelled.
func attNativePick(ctx context.Context) (paths []string, ok bool, err error) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil, false, nil
	}
	home, _ := os.UserHomeDir()
	switch {
	case attHave("zenity"):
		args := []string{"--file-selection", "--multiple", "--separator=\n", "--title=Upload files"}
		if home != "" {
			args = append(args, "--filename="+home+"/")
		}
		out, err := exec.CommandContext(ctx, "zenity", args...).Output()
		return attPickResult(out, err)
	case attHave("kdialog"):
		if home == "" {
			home = "/"
		}
		out, err := exec.CommandContext(ctx, "kdialog", "--title", "Upload files",
			"--getopenfilename", home, "--multiple", "--separate-output").Output()
		return attPickResult(out, err)
	}
	return nil, false, nil
}
