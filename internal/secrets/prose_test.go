package secrets

import "testing"

// TestScanCatchesTheAuditCorpus runs the ten formats from A-05 of the 2026-09-02
// audit. Eight were already blocked; s08 and s09 went through to the remote, and
// they are the two shapes a memory system actually produces, because a memory
// file is prose rather than configuration.
func TestScanCatchesTheAuditCorpus(t *testing.T) {
	corpus := []struct{ name, text string }{
		{"s01 aws access key id", "AKIAIOSFODNN7EXAMPLE"},
		{"s02 assigned secret", "secret = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},
		// s03 and s04 are shortened and de-vendored on purpose. Written the way
		// the audit wrote them, GitHub's push protection recognises them as a
		// real Slack token and a real Stripe key and refuses the push, so this
		// file could never reach the remote. These still exercise the same two
		// rules — the xox- prefix and the entropy heuristic — without carrying a
		// string that any scanner has to treat as a live credential.
		{"s03 slack token", "xoxb-SYNTHETIC-FIXTURE-NOT-A-TOKEN"},
		{"s04 high-entropy string", "Zq7NGgRy8XU2fEdT1kWvBc4MsJp0LnHt6Yi3Ae9Ou"},
		{"s05 private key block", "-----BEGIN RSA PRIVATE KEY-----"},
		{"s06 credential in URL", "postgres://admin:S3cretPassw0rd@db.example.com:5432/app"},
		{"s07 jwt", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"},
		{"s08 password in prose", "my password is hunter2correcthorse"},
		{"s09 password in prose with a colon", "The API password for the staging box is: Tr0ub4dor&3"},
		{"s10 ssh public key", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDZ8kLm2nPqRsTuVwXyZ0123456789AbCdEfGh"},
	}

	for _, c := range corpus {
		if got := Scan(c.text); len(got) == 0 {
			t.Errorf("%s: Scan found nothing in %q", c.name, c.text)
		}
	}
}

// TestScanProseAcceptsSentencesAboutPasswords is the other half of the rule, and
// the half that decides whether it survives contact with real memory files. A
// blocking rule that fires on a sentence merely discussing a password would be
// switched off within a week.
func TestScanProseAcceptsSentencesAboutPasswords(t *testing.T) {
	clean := []string{
		"the password is configured in the deployment pipeline",
		"the password is rotated-every-90-days",
		"la contraseña es 1Password",
		"the database password lives in the vault, not here",
		"password rotation is documented in the runbook",
		"the passphrase is stored in a hardware key",
	}
	for _, line := range clean {
		for _, f := range Scan(line) {
			if f.Rule == "password in prose" {
				t.Errorf("false positive on %q: %s", line, f)
			}
		}
	}
}

// TestScanProseReportsItsOwnRule keeps a blocked memory diagnosable: "8 blocked"
// with no rule name makes a false positive impossible to tell from a real catch.
func TestScanProseReportsItsOwnRule(t *testing.T) {
	got := Scan("my password is hunter2correcthorse")
	for _, f := range got {
		if f.Rule == "password in prose" {
			if f.Line != 1 {
				t.Errorf("Line = %d, want 1", f.Line)
			}
			return
		}
	}
	t.Errorf(`no finding named "password in prose"; got %v`, got)
}

// TestRedactURL covers the helper the three leak paths of A-03 now share.
func TestRedactURL(t *testing.T) {
	cases := []struct{ raw, want string }{
		{
			"https://arlezz:github_pat_11ABCDEFG0aBcDeFgHiJkL@github.com/acme-dev/orbit-x-memory.git",
			"https://***@github.com/acme-dev/orbit-x-memory.git",
		},
		{
			"ssh://git@gitlab.example.com:2222/acme-dev/orbit-x-memory.git",
			"ssh://***@gitlab.example.com:2222/acme-dev/orbit-x-memory.git",
		},
		{"git@github.com:acme-dev/orbit-x-memory.git", "***@github.com:acme-dev/orbit-x-memory.git"},
		// Nothing to redact: left exactly as it was.
		{"https://github.com/acme-dev/orbit-x-memory.git", "https://github.com/acme-dev/orbit-x-memory.git"},
		{"", ""},
	}
	for _, c := range cases {
		if got := RedactURL(c.raw); got != c.want {
			t.Errorf("RedactURL(%q) = %q, want %q", c.raw, got, c.want)
		}
		// Idempotence is what lets the redacted form be compared against itself
		// wherever the raw one used to be, in the manifest above all.
		if got := RedactURL(RedactURL(c.raw)); got != c.want {
			t.Errorf("RedactURL is not idempotent on %q: %q", c.raw, got)
		}
	}
}
