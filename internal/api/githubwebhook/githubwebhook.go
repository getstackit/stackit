// Package githubwebhook contains the pure, transport-free pieces of the GitHub
// webhook receiver: verifying a delivery's HMAC signature and extracting the
// repository coordinates from a push payload. Keeping these out of the HTTP
// handler lets them be unit-tested without a server and keeps the handler thin.
package githubwebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SignatureHeader is the request header GitHub signs each delivery with.
const SignatureHeader = "X-Hub-Signature-256"

// EventHeader names the webhook event type (e.g. "push", "ping").
const EventHeader = "X-GitHub-Event"

// DeliveryHeader carries GitHub's per-delivery UUID. Logging it lets an
// operator correlate a server-side log line with the matching entry in the
// webhook's "Recent Deliveries" list on GitHub.
const DeliveryHeader = "X-GitHub-Delivery"

const signaturePrefix = "sha256="

// Verify reports whether sigHeader is a valid HMAC-SHA256 signature of body
// under secret, in GitHub's "sha256=<hex>" form. The comparison is
// constant-time. An empty secret or header is treated as invalid so a
// misconfigured receiver fails closed rather than accepting unsigned payloads.
func Verify(secret string, body []byte, sigHeader string) bool {
	if secret == "" || sigHeader == "" {
		return false
	}
	if !strings.HasPrefix(sigHeader, signaturePrefix) {
		return false
	}
	want := sigHeader[len(signaturePrefix):]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := hex.EncodeToString(mac.Sum(nil))

	// hmac.Equal is constant-time over equal-length inputs; the lengths match
	// whenever want is a valid sha256 hex digest, and differ harmlessly
	// otherwise.
	return hmac.Equal([]byte(got), []byte(want))
}

// pushPayload captures only the repository coordinates we need from a GitHub
// push event. Owner login and repo name identify the checkout to refresh.
type pushPayload struct {
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
}

// ParsePush extracts the owner and repository name from a push event body. It
// prefers the explicit owner.login / repository.name fields and falls back to
// splitting full_name ("owner/repo"). ok is false when the body isn't a
// recognizable push payload, so the caller can ignore it without erroring.
func ParsePush(body []byte) (owner, name string, ok bool) {
	var p pushPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return "", "", false
	}

	owner = p.Repository.Owner.Login
	if owner == "" {
		owner = p.Repository.Owner.Name
	}
	name = p.Repository.Name

	if (owner == "" || name == "") && p.Repository.FullName != "" {
		if o, n, found := strings.Cut(p.Repository.FullName, "/"); found {
			if owner == "" {
				owner = o
			}
			if name == "" {
				name = n
			}
		}
	}

	if owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}
