package desktop

import "strings"

// URLHandlerResult is what a slack:// handler registration found and did
// (Windows; other systems register through their packages).
type URLHandlerResult struct {
	// Changed: something was written. False: Clack was already
	// registered exactly so, and nothing was touched.
	Changed bool
	// Default: the slack:// links Windows opens without asking go to this
	// Clack (the legacy Software\Classes\slack key is Clack's). False:
	// Owner has them, and Windows either asks which app to use or uses the
	// one the user chose.
	Default bool
	// Owner is the other app, when Default is false: its executable, or
	// the ProgID the user chose.
	Owner string
}

// IsURLLaunch reports whether args (os.Args) are a URL handler's launch:
// the program, then a slack: link. Windows runs `"Clack.exe" "%1"`, and
// clack.desktop's Exec is `clack %u`.
func IsURLLaunch(args []string) bool {
	return len(args) >= 2 && strings.HasPrefix(strings.ToLower(args[1]), "slack:")
}

// URLLaunchArgs keeps only the program and the link of a URL handler's
// launch. A link with a quote in it splits the command line into more
// arguments ("Registering an Application to a URI Scheme", Microsoft
// Learn: handlers "should assume that any parameters on the command line
// could come from malicious parties"), so nothing after the link may be
// read as a flag, a subcommand or --update. Other launches pass as they
// are.
func URLLaunchArgs(args []string) []string {
	if IsURLLaunch(args) {
		return args[:2:2]
	}
	return args
}
