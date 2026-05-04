package server

import "bytes"

// stripANSI removes ANSI escape sequences from b, returning (originalBytes,
// strippedString). The first return is a copy of the input (caller may release
// the slice safely).
func stripANSI(b []byte) ([]byte, string) {
	original := make([]byte, len(b))
	copy(original, b)
	var out bytes.Buffer
	out.Grow(len(b))
	i := 0
	for i < len(b) {
		c := b[i]
		// CSI (Control Sequence Introducer) — most common: ESC [ ... letter.
		if c == 0x1B && i+1 < len(b) && b[i+1] == '[' {
			j := i + 2
			for j < len(b) && !((b[j] >= '@' && b[j] <= '~')) {
				j++
			}
			if j < len(b) {
				j++
			}
			i = j
			continue
		}
		// OSC (Operating System Command) — ESC ] ... BEL or ESC \.
		if c == 0x1B && i+1 < len(b) && b[i+1] == ']' {
			j := i + 2
			for j < len(b) {
				if b[j] == 0x07 { // BEL
					j++
					break
				}
				if b[j] == 0x1B && j+1 < len(b) && b[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
			continue
		}
		out.WriteByte(c)
		i++
	}
	return original, out.String()
}
