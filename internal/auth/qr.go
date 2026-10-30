package auth

import (
	"strings"

	"rsc.io/qr"
)

// TerminalQR renders text as a QR code for a terminal. Two QR rows share one line via
// "▀" with explicit black/white colours, so it scans on dark and light themes alike.
func TerminalQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 2
	n := code.Size + 2*quiet
	black := func(x, y int) bool { return code.Black(x-quiet, y-quiet) }
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			fg, bg := "37", "47" // white top / white bottom
			if black(x, y) {
				fg = "30"
			}
			if y+1 < n && black(x, y+1) {
				bg = "40"
			}
			b.WriteString("\x1b[" + fg + ";" + bg + "m▀")
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}
