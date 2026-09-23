package redact

import (
	"errors"
	"testing"
)

func TestValues_RedactsMatchingValue(t *testing.T) {
	got := Values("token=abcdefgh12 done", []string{"abcdefgh12"})
	if got != "token=*** done" {
		t.Errorf("got %q", got)
	}
}

func TestValues_ShortValueUntouched(t *testing.T) {
	// "short" is under MinSecretLen and must not be redacted - see D5's
	// documented (not a bug) behavior.
	got := Values("short value here", []string{"short"})
	if got != "short value here" {
		t.Errorf("got %q, want unredacted (value below MinSecretLen)", got)
	}
}

func TestValues_LongestFirst(t *testing.T) {
	// "abcdefghij" contains "abcdefgh" as a prefix; the longer value must be
	// matched first so no fragment of it survives redaction.
	got := Values("abcdefghij", []string{"abcdefgh", "abcdefghij"})
	if got != "***" {
		t.Errorf("got %q, want the whole longer match redacted first", got)
	}
}

func TestValues_OverlappingNonNestedSecretsBothRedacted(t *testing.T) {
	// "abcdefgh" and "efghijkl" overlap in "efgh" without either containing
	// the other. A sequential ReplaceAll per value would consume the shared
	// bytes on the first match and hide the second value's match entirely -
	// see REVIEW.md R3. Both must be fully redacted as one merged run.
	got := Values("prefix abcdefghijkl suffix", []string{"abcdefgh", "efghijkl"})
	if got != "prefix *** suffix" {
		t.Errorf("got %q, want the overlapping run redacted as one ***", got)
	}
}

func TestValues_NoMatchingSecretLeavesStringUnchanged(t *testing.T) {
	got := Values("nothing sensitive here", []string{"absentsecret1"})
	if got != "nothing sensitive here" {
		t.Errorf("got %q, want unchanged when no value matches", got)
	}
}

func TestValues_MultipleDisjointSecretsBothRedacted(t *testing.T) {
	got := Values("start firstsecret1 middle secondsecret2 end", []string{"firstsecret1", "secondsecret2"})
	if got != "start *** middle *** end" {
		t.Errorf("got %q, want each disjoint secret redacted separately", got)
	}
}

func TestValues_EmptyValuesIsNoop(t *testing.T) {
	got := Values("nothing to redact here", nil)
	if got != "nothing to redact here" {
		t.Errorf("got %q", got)
	}
}

func TestPrepare_DropsShortAndDeduplicates(t *testing.T) {
	got := Prepare([]string{"short", "longenough1", "longenough1", "alsolongenough2"})
	if len(got) != 2 {
		t.Fatalf("got %d values, want 2 (short dropped, duplicate removed): %v", len(got), got)
	}
	if len(got[0]) < len(got[1]) {
		t.Errorf("got %v, want longest-first order", got)
	}
}

func TestValuesPrepared_MatchesValues(t *testing.T) {
	prepared := Prepare([]string{"secretvalue1"})
	got := ValuesPrepared("prefix secretvalue1 suffix", prepared)
	if got != "prefix *** suffix" {
		t.Errorf("got %q", got)
	}
}

func TestErr_NilReturnsEmptyString(t *testing.T) {
	if got := Err(nil, []string{"secretvalue1"}); got != "" {
		t.Errorf("Err(nil, ...) = %q, want empty string", got)
	}
}

func TestErr_RedactsErrorMessage(t *testing.T) {
	err := errors.New("failed with token abcdefgh12 in response")
	got := Err(err, []string{"abcdefgh12"})
	if got != "failed with token *** in response" {
		t.Errorf("got %q", got)
	}
}

func TestErr_NoKnownSecretsIsPlainMessage(t *testing.T) {
	err := errors.New("plain failure, nothing to redact")
	got := Err(err, nil)
	if got != "plain failure, nothing to redact" {
		t.Errorf("got %q", got)
	}
}
