package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"kestrix/internal/config"
	"kestrix/internal/credentials"
	"kestrix/internal/store"
)

// cmdKeys implements `kestrix keys <rotate-master>`. It connects to PostgreSQL
// directly to perform offline key management operations while the server is stopped.
func cmdKeys(args []string) {
	code := runKeys(args, os.Stdout, os.Stderr)
	if code != 0 {
		os.Exit(code)
	}
}

func runKeys(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: kestrix keys rotate-master --new-key-path PATH [--database-url postgres://...]")
		return 2
	}
	switch args[0] {
	case "rotate-master":
		return runKeysRotateMaster(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown keys subcommand: %s\n", args[0])
		return 2
	}
}

func runKeysRotateMaster(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("keys rotate-master", flag.ContinueOnError)
	fs.SetOutput(stderr)
	newKeyPath := fs.String("new-key-path", "", "path to new 0600 master key file")
	dbURL := fs.String("database-url", "", "PostgreSQL connection URL (env: DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cleanNewKeyPath := strings.TrimSpace(*newKeyPath)
	if cleanNewKeyPath == "" {
		fmt.Fprintln(stderr, "error: --new-key-path is required")
		return 2
	}

	currentKeyPath := strings.TrimSpace(config.Current().MasterKeyPath)
	if currentKeyPath == "" {
		fmt.Fprintln(stderr, "error: KESTRIX_MASTER_KEY_PATH is required")
		return 1
	}

	oldProvider := credentials.FileMasterKeyProvider{Path: currentKeyPath, Version: 1}
	oldVersion, oldKey, err := oldProvider.Current()
	if err != nil {
		fmt.Fprintf(stderr, "error: current master key: %v\n", err)
		return 1
	}

	newProvider := credentials.FileMasterKeyProvider{Path: cleanNewKeyPath, Version: oldVersion + 1}
	_, newKey, err := newProvider.Current()
	if err != nil {
		fmt.Fprintf(stderr, "error: new master key: %v\n", err)
		return 1
	}

	if bytes.Equal(oldKey, newKey) {
		fmt.Fprintln(stderr, "error: new key must differ from current key")
		return 1
	}

	targetDBURL := resolveUserDBURL(*dbURL)
	pg, err := store.NewPG(targetDBURL, "")
	if err != nil {
		fmt.Fprintf(stderr, "error: connect to database: %v\n", err)
		return 1
	}
	defer pg.Close()

	ctx := context.Background()
	locked, lockCloser, err := pg.TryServerLock(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "error: acquire advisory lock: %v\n", err)
		return 1
	}
	if !locked {
		fmt.Fprintln(stderr, "error: a Kestrix server is connected to this database; stop it first")
		return 1
	}
	defer lockCloser.Close()

	counts, err := pg.RotateMasterKey(ctx, oldProvider, newProvider)
	if err != nil {
		fmt.Fprintf(stderr, "error: key rotation failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, `Master key rotation completed successfully:
  credentials: %d
  remote job secrets: %d
  run secrets: %d

Replace KESTRIX_MASTER_KEY_PATH with %s before starting Kestrix.
`, counts.Credentials, counts.RemoteJobSecrets, counts.RunSecrets, cleanNewKeyPath)

	return 0
}
