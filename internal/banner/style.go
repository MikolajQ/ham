package banner

import "os"

// glamourStyle returns the glamour style for banners. "auto" makes termenv ask the
// terminal for its background colour (OSC 10/11) and wait for the answer; under
// `script`/`nohup` (the documented way to run `ham get` unattended) the pty exists but
// nobody answers, so ham hung forever on the first banner. Default to a fixed style;
// HAM_STYLE=auto (or light/dark/notty) opts back in.
func glamourStyle() string {
	if s := os.Getenv("HAM_STYLE"); s != "" {
		return s
	}
	return "dark"
}
