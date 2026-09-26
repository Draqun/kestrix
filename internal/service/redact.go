package service

import (
	"io"

	"kestrix/internal/redact"
)

// redactingWriter is redact.Writer: buffers across writes so a value split
// between two writes is still matched. It moved to internal/redact so
// `kestrix runner` can share it (remote-runner-fixes design.md D1).
type redactingWriter = redact.Writer

func newRedactingWriter(w io.Writer, values []string) *redactingWriter {
	return redact.NewWriter(w, values)
}

// redactValues replaces every occurrence of each configured secret value
// (see prepareRedactionValues) in s with "***". Used where the text is
// already a complete string - TaskResult.Output and ApplyTaskResult's
// outputExtra - so no cross-write buffering is needed.
func redactValues(s string, values []string) string {
	return redact.Values(s, values)
}
