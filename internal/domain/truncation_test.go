package domain

import "testing"

// The text is asserted literally: it is what an operator reads at the end of a
// cut output, in-process and remote alike.
func TestTruncationMarker(t *testing.T) {
	for _, tc := range []struct {
		max  int
		want string
	}{
		{10, "--- output truncated at 10 bytes (runner.max_output_bytes) ---\n"},
		{10 << 20, "--- output truncated at 10485760 bytes (runner.max_output_bytes) ---\n"},
	} {
		if got := TruncationMarker(tc.max); got != tc.want {
			t.Fatalf("TruncationMarker(%d) = %q, want %q", tc.max, got, tc.want)
		}
	}
}
