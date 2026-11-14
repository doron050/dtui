package tui

import (
	"strings"
	"unicode/utf8"
)

// sanitizeFrame is the last guard between record data and the terminal. It
// passes printable text, newlines, and SGR styling (ESC[…m, the only escape
// lipgloss emits) and drops every other control sequence and control
// character: OSC strings (clipboard, window title, hyperlinks), DCS/APC/PM/SOS
// strings, cursor and erase CSI, two-byte escapes, C1 controls, and bare C0
// bytes. Telemetry text that slipped past catalog.SafeText can then style a
// cell at most — never drive the terminal.
func sanitizeFrame(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			i = frameEscape(&b, s, i)
		case c == '\n':
			b.WriteByte(c)
			i++
		case c == '\t':
			b.WriteByte(' ')
			i++
		case c < 0x20 || c == 0x7f:
			i++
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			switch {
			case r == utf8.RuneError && n == 1:
				b.WriteRune(utf8.RuneError)
			case r >= 0x80 && r <= 0x9f:
			default:
				b.WriteString(s[i : i+n])
			}
			i += n
		}
	}
	return b.String()
}

// frameEscape handles the escape sequence starting at s[i] (an ESC byte):
// SGR is copied through, anything else is skipped. It returns the index just
// past what it consumed.
func frameEscape(b *strings.Builder, s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		j := i + 2
		for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3f {
			j++
		}
		params := s[i+2 : j]
		k := j
		for k < len(s) && s[k] >= 0x20 && s[k] <= 0x2f {
			k++
		}
		if k >= len(s) || s[k] < 0x40 || s[k] > 0x7e {
			return k // malformed: drop the introducer and parameters only
		}
		if s[k] == 'm' && k == j && strings.Trim(params, "0123456789;:") == "" {
			b.WriteString(s[i : k+1])
		}
		return k + 1
	case ']', 'P', '_', '^', 'X':
		// Control strings end at BEL or ST (ESC \). Their payload is dropped
		// up to the terminator, a newline, or the next escape — past the
		// dropped introducer, any remainder is inert text.
		for j := i + 2; j < len(s); j++ {
			switch s[j] {
			case 0x07:
				return j + 1
			case '\n':
				return j
			case 0x1b:
				if j+1 < len(s) && s[j+1] == '\\' {
					return j + 2
				}
				return j
			}
		}
		return len(s)
	}
	// Two-byte (or intermediate-prefixed) escapes: ESC c, ESC 7, ESC ( B, …
	j := i + 1
	for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
		j++
	}
	if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
		return j + 1
	}
	return j
}
