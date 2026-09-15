package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"kestrix/internal/mocks"
)

func TestReconcileInterruptedRuns_LogsCount(t *testing.T) {
	store := &mocks.MockRunStore{FailInterruptedResult: 3}
	var out bytes.Buffer

	if err := reconcileInterruptedRuns(store, &out); err != nil {
		t.Fatalf("reconcileInterruptedRuns: %v", err)
	}

	if len(store.FailInterruptedCalls) != 1 || store.FailInterruptedCalls[0] != "interrupted by server restart" {
		t.Fatalf("FailInterruptedCalls = %v, want one call with the restart reason", store.FailInterruptedCalls)
	}
	if got := out.String(); !strings.Contains(got, "runs: failed 3 interrupted run(s) from a previous instance") {
		t.Fatalf("log output = %q, want it to report the count", got)
	}
}

func TestReconcileInterruptedRuns_LogsZero(t *testing.T) {
	store := &mocks.MockRunStore{}
	var out bytes.Buffer

	if err := reconcileInterruptedRuns(store, &out); err != nil {
		t.Fatalf("reconcileInterruptedRuns: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "runs: failed 0 interrupted run(s) from a previous instance") {
		t.Fatalf("log output = %q, want it to report zero", got)
	}
}

func TestReconcileInterruptedRuns_StoreErrorNamesTheStep(t *testing.T) {
	store := &mocks.MockRunStore{FailInterruptedErr: errors.New("connection refused")}
	var out bytes.Buffer

	err := reconcileInterruptedRuns(store, &out)
	if err == nil {
		t.Fatal("expected an error when the store fails")
	}
	if !strings.Contains(err.Error(), "reconcile interrupted runs") {
		t.Fatalf("error = %q, want it to name the reconcile step", err.Error())
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %q, want the underlying store error", err.Error())
	}
	if out.Len() != 0 {
		t.Fatalf("log output = %q, want nothing logged on failure", out.String())
	}
}
