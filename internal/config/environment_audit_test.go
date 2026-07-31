package config

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeEnvironmentReadsAreCentralized(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.Clean(path) == filepath.Join(root, "internal", "config", "environment.go") {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			lineNumber := 0
			for scanner.Scan() {
				lineNumber++
				line := scanner.Text()
				for _, expression := range []string{"os.Getenv(", "os.LookupEnv(", "os.Environ("} {
					if strings.Contains(line, expression) {
						t.Errorf("direct environment read %s:%d: %s", path, lineNumber, strings.TrimSpace(line))
					}
				}
			}
			return scanner.Err()
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
}
