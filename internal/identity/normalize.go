package identity

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ErrLocalRemote reports a remote that points at a local path.
//
// Deriving identity from a local path is the exact failure this tool fixes, so
// such a remote is refused rather than accepted with a path-shaped slug.
var ErrLocalRemote = errors.New("remote points at a local path, which cannot identify a project across machines")

// scpLike matches git's scp-style syntax host:path, with no scheme and no slash
// before the colon. Credentials are removed before this is applied, so the
// pattern does not have to model them.
var scpLike = regexp.MustCompile(`^([^/:]+):(.+)$`)

// winDrive matches a Windows drive-letter path, e.g. "C:\repos\x" or "C:/repos/x".
var winDrive = regexp.MustCompile(`^[A-Za-z]:[/\\]`)

// Normalize turns a raw git remote URL into a canonical, credential-free
// project identity of the form "host/path/to/repo".
//
// Everything that can differ between two clones of the same repository is
// discarded: scheme, credentials, port, the ".git" suffix, and letter case.
// Two developers reaching the same repo over SSH and HTTPS must land on the
// same identity, otherwise their memory does not merge.
//
// Stripping credentials is a security requirement, not a cosmetic one. The
// canonical value gets written into slugs, manifests and log lines, and one of
// the remotes in real use here carries an access token inline.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty remote URL")
	}

	// Reject bare local paths before any parsing: net/url happily accepts them
	// and "C:" would otherwise be read as a scheme.
	if winDrive.MatchString(raw) || strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, ".") || strings.HasPrefix(raw, `\`) {
		return "", ErrLocalRemote
	}

	host, path, err := split(raw)
	if err != nil {
		return "", err
	}

	host = strings.ToLower(strings.TrimSpace(host))
	// Drop the port: the same repository is the same project whether it is
	// reached on 22, 443 or 2222.
	if i := strings.LastIndex(host, ":"); i != -1 {
		host = host[:i]
	}
	if host == "" {
		return "", ErrLocalRemote
	}

	path = strings.Trim(strings.ReplaceAll(path, `\`, "/"), "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if path == "" {
		return "", fmt.Errorf("remote %q has no repository path", redact(raw))
	}

	return strings.ToLower(host + "/" + path), nil
}

// split separates a remote into host and path, handling both URL schemes and
// git's scp-style syntax.
func split(raw string) (host, path string, err error) {
	if !strings.Contains(raw, "://") {
		// Credentials must come off before the host is read. A password
		// containing a colon otherwise makes the user name look like the host,
		// which would put the token itself into the identity.
		if m := scpLike.FindStringSubmatch(stripSCPUserinfo(raw)); m != nil {
			return m[1], m[2], nil
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("unparseable remote %q", redact(raw))
	}
	switch strings.ToLower(u.Scheme) {
	case "file", "":
		return "", "", ErrLocalRemote
	}
	// u.User is dropped here, and this is the only place credentials are held.
	return u.Host, u.Path, nil
}

// stripSCPUserinfo removes any "user:password@" prefix from an scp-style remote.
//
// Only the part before the first slash is considered, so an "@" inside the
// repository path is left alone.
func stripSCPUserinfo(raw string) string {
	end := strings.IndexByte(raw, '/')
	if end == -1 {
		end = len(raw)
	}
	if at := strings.LastIndexByte(raw[:end], '@'); at != -1 {
		return raw[at+1:]
	}
	return raw
}

// slugUnsafe matches every character not allowed in a slug component.
var slugUnsafe = regexp.MustCompile(`[^a-z0-9._-]+`)

// Slugify converts a canonical identity into a filesystem-safe directory name.
//
// Path separators become "__" so the slug stays readable — a hash would be
// shorter but would make the on-disk layout impossible to inspect by hand,
// and being able to eyeball the store is what makes a future migration to a
// server-backed index safe.
func Slugify(canonical string) string {
	s := strings.ToLower(strings.TrimSpace(canonical))
	s = strings.ReplaceAll(s, `\`, "/")
	parts := strings.Split(s, "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		p = slugUnsafe.ReplaceAllString(p, "-")
		p = strings.Trim(p, "-")
		if p != "" {
			clean = append(clean, p)
		}
	}
	return strings.Join(clean, "__")
}

// redact removes any inline credentials from a remote URL so it is safe to put
// in an error message.
func redact(raw string) string {
	if i := strings.Index(raw, "://"); i != -1 {
		rest := raw[i+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			return raw[:i+3] + "***@" + rest[at+1:]
		}
		return raw
	}
	if at := strings.Index(raw, "@"); at != -1 {
		return "***@" + raw[at+1:]
	}
	return raw
}
