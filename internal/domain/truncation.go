package domain

import "fmt"

// TruncationMarker is the line that ends output cut at max bytes. It names the
// bound so an operator knows how to raise it. In-process output
// (internal/service) and remote output (internal/store) end with this same
// line; each layer decides where to cut, this decides only what the cut says.
func TruncationMarker(max int) string {
	return fmt.Sprintf("--- output truncated at %d bytes (runner.max_output_bytes) ---\n", max)
}
