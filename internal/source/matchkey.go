package source

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// scpLikeURL matches the shorthand Git accepts without a scheme:
// [user@]host:path. A path that starts with "/" is not scp-like.
var scpLikeURL = regexp.MustCompile(`^(?:([^@/:]+)@)?([^@/:]+):([^/].*)$`)

// SourceMatchKey reduces a repository URL to the identity webhook routing
// compares: lowercase host and path, without the scheme, an SSH user name, a
// ".git" suffix, or a trailing slash. The HTTPS and SSH clone forms of one
// repository therefore yield the same key, which canonicalSourceURL must not
// do because the cache keeps transports apart. A URL with embedded
// credentials is rejected rather than matched.
func SourceMatchKey(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if !strings.Contains(clean, "://") {
		if m := scpLikeURL.FindStringSubmatch(clean); m != nil {
			clean = "ssh://" + m[2] + "/" + m[3]
		}
	}
	canonical, err := canonicalSourceURL(clean)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(canonical)
	if err != nil {
		return "", fmt.Errorf("invalid remote source URL")
	}
	// Lowercase before trimming: TrimSuffix is case-sensitive, so a ".GIT"
	// left in place would key the same repository differently than ".git".
	path := strings.TrimSuffix(strings.ToLower(strings.Trim(parsed.Path, "/")), ".git")
	if path == "" {
		return "", fmt.Errorf("remote source URL has no repository path")
	}
	return strings.ToLower(parsed.Hostname()) + portSuffix(parsed) + "/" + path, nil
}

// defaultPorts are dropped from the key so an explicit default port does not
// split a repository from its plain form.
var defaultPorts = map[string]string{"https": "443", "http": "80", "ssh": "22", "git": "9418"}

func portSuffix(u *url.URL) string {
	if p := u.Port(); p != "" && p != defaultPorts[u.Scheme] {
		return ":" + p
	}
	return ""
}
