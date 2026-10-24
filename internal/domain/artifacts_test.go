package domain

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateArtifacts_Valid(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"dist/"},
		{"dist/app.txt", "out/bundle.tar.gz"},
		{"a", "b/c", "d/e/f"},
	}
	for _, tc := range cases {
		if err := ValidateArtifacts("test-task", tc); err != nil {
			t.Errorf("ValidateArtifacts(%v) = %v, want nil", tc, err)
		}
	}
}

func TestValidateArtifacts_EmptyEntry(t *testing.T) {
	cases := [][]string{
		{""},
		{"   "},
		{"dist/", ""},
	}
	for _, tc := range cases {
		err := ValidateArtifacts("build", tc)
		if err == nil || !strings.Contains(err.Error(), "artifact path cannot be empty") {
			t.Errorf("ValidateArtifacts(%v) = %v, want error naming cannot be empty", tc, err)
		}
	}
}

func TestValidateArtifacts_AbsolutePath(t *testing.T) {
	cases := [][]string{
		{"/etc/passwd"},
		{"/dist"},
		{"dist/", "/app/out"},
	}
	for _, tc := range cases {
		err := ValidateArtifacts("build", tc)
		if err == nil || !strings.Contains(err.Error(), "path must be relative") {
			t.Errorf("ValidateArtifacts(%v) = %v, want error naming relative", tc, err)
		}
	}
}

func TestValidateArtifacts_DotDotTraversal(t *testing.T) {
	cases := [][]string{
		{".."},
		{"../secrets"},
		{"dist/../secret"},
		{"a/b/.."},
		{"dist/", "sub/../../etc"},
	}
	for _, tc := range cases {
		err := ValidateArtifacts("build", tc)
		if err == nil || !strings.Contains(err.Error(), "must not contain '..' components") {
			t.Errorf("ValidateArtifacts(%v) = %v, want error naming '..' components", tc, err)
		}
	}
}

func TestValidateArtifacts_MaxEntries(t *testing.T) {
	var valid []string
	for i := 0; i < MaxTaskArtifacts; i++ {
		valid = append(valid, fmt.Sprintf("file_%d", i))
	}
	if err := ValidateArtifacts("build", valid); err != nil {
		t.Fatalf("ValidateArtifacts(32 entries) = %v, want nil", err)
	}

	invalid := append(valid, "file_32")
	err := ValidateArtifacts("build", invalid)
	if err == nil || !strings.Contains(err.Error(), "at most 32 entries allowed") {
		t.Fatalf("ValidateArtifacts(33 entries) = %v, want error naming at most 32 entries allowed", err)
	}
}
