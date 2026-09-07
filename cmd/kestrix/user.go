package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"kestrix/internal/auth"
	"kestrix/internal/config"
	"kestrix/internal/domain"
	"kestrix/internal/store"
)

// cmdUser implements `kestrix user <create|list|passwd|delete>`. It talks to
// PostgreSQL directly, the same way `kestrix serve` does, so it works
// without a reachable API or an existing account - see
// user-authentication spec, "Account management from the command line".
func cmdUser(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: kestrix user <create|list|passwd|delete> ...")
		os.Exit(2)
	}
	switch args[0] {
	case "create":
		cmdUserCreate(args[1:])
	case "list":
		cmdUserList(args[1:])
	case "passwd":
		cmdUserPasswd(args[1:])
	case "delete":
		cmdUserDelete(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown user subcommand: %s\n", args[0])
		os.Exit(2)
	}
}

// resolveUserDBURL applies the same precedence cmdServe uses: an explicit
// flag, then DATABASE_URL, then the local development default.
func resolveUserDBURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := config.Current().DatabaseURL; env != "" {
		return env
	}
	return defaultLocalDatabaseURL
}

// openUserStore connects to PostgreSQL for one CLI invocation. The
// connection is not explicitly closed: the process exits immediately after
// its one command runs, and the OS reclaims it.
func openUserStore(dbURL string) *store.PGStore {
	pg, err := store.NewPG(dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to database: %v\n", err)
		os.Exit(1)
	}
	return pg
}

// readPassword reads a password from a terminal without echoing it, or from
// standard input when not attached to a terminal (piped input, scripting,
// or tests) - never from a command-line flag, so it cannot appear in the
// process table or shell history.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		bytePassword, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(bytePassword), nil
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func cmdUserCreate(args []string) {
	fs := flag.NewFlagSet("user create", flag.ExitOnError)
	role := fs.String("role", "viewer", "global role: viewer, operator, or admin")
	dbURL := fs.String("database-url", "", "PostgreSQL connection URL (env: DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: kestrix user create [--role viewer|operator|admin] <name>")
		os.Exit(2)
	}
	name := fs.Arg(0)
	r := domain.Role(*role)
	if !r.Valid() {
		fmt.Fprintf(os.Stderr, "invalid role %q: must be viewer, operator, or admin\n", *role)
		os.Exit(2)
	}

	password, err := readPassword(fmt.Sprintf("Password for %s: ", name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if password == "" {
		fmt.Fprintln(os.Stderr, "password must not be empty")
		os.Exit(2)
	}

	pg := openUserStore(resolveUserDBURL(*dbURL))

	hash, err := auth.NewArgon2Hasher().Hash(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}
	u, err := pg.CreateUser(name, r, hash)
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			fmt.Fprintf(os.Stderr, "user %q already exists\n", name)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "create user: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("created user %q with role %s\n", u.Name, u.Role)
}

func cmdUserList(args []string) {
	fs := flag.NewFlagSet("user list", flag.ExitOnError)
	dbURL := fs.String("database-url", "", "PostgreSQL connection URL (env: DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	pg := openUserStore(resolveUserDBURL(*dbURL))

	users, err := pg.ListUsers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list users: %v\n", err)
		os.Exit(1)
	}
	if len(users) == 0 {
		fmt.Println("no accounts")
		return
	}
	fmt.Printf("%-24s %-10s %s\n", "NAME", "ROLE", "CREATED")
	for _, u := range users {
		fmt.Printf("%-24s %-10s %s\n", u.Name, u.Role, u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
}

func cmdUserPasswd(args []string) {
	fs := flag.NewFlagSet("user passwd", flag.ExitOnError)
	dbURL := fs.String("database-url", "", "PostgreSQL connection URL (env: DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: kestrix user passwd <name>")
		os.Exit(2)
	}
	name := fs.Arg(0)

	password, err := readPassword(fmt.Sprintf("New password for %s: ", name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if password == "" {
		fmt.Fprintln(os.Stderr, "password must not be empty")
		os.Exit(2)
	}

	pg := openUserStore(resolveUserDBURL(*dbURL))

	hash, err := auth.NewArgon2Hasher().Hash(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}
	if err := pg.UpdateUserPassword(name, hash); err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			fmt.Fprintf(os.Stderr, "user %q not found\n", name)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "update password: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("updated password for %q\n", name)
}

func cmdUserDelete(args []string) {
	fs := flag.NewFlagSet("user delete", flag.ExitOnError)
	dbURL := fs.String("database-url", "", "PostgreSQL connection URL (env: DATABASE_URL)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: kestrix user delete <name>")
		os.Exit(2)
	}
	name := fs.Arg(0)

	pg := openUserStore(resolveUserDBURL(*dbURL))

	if err := pg.DeleteUser(name); err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			fmt.Fprintf(os.Stderr, "user %q not found\n", name)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "delete user: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("deleted user %q\n", name)
}
