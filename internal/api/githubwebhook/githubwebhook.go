// Package githubwebhook contains the pure, transport-free pieces of the GitHub
// webhook receiver: verifying a delivery's HMAC signature and extracting the
// repository coordinates from a push payload. Keeping these out of the HTTP
// handler lets them be unit-tested without a server and keeps the handler thin.
package githubwebhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/getstackit/stackit/internal/api/registry"
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

// ParsePush extracts the repository coordinates from a push event body. It
// prefers the explicit owner.login / repository.name fields and falls back to
// splitting full_name ("owner/repo"). ok is false when the body isn't a
// recognizable push payload, so the caller can ignore it without erroring.
func ParsePush(body []byte) (repo registry.RepoRef, ok bool) {
	var p pushPayload
	if err := json.Unmarshal(normalizePushBody(body), &p); err != nil {
		return registry.RepoRef{}, false
	}

	repo.Owner = p.Repository.Owner.Login
	if repo.Owner == "" {
		repo.Owner = p.Repository.Owner.Name
	}
	repo.Name = p.Repository.Name

	if (repo.Owner == "" || repo.Name == "") && p.Repository.FullName != "" {
		if o, n, found := strings.Cut(p.Repository.FullName, "/"); found {
			if repo.Owner == "" {
				repo.Owner = o
			}
			if repo.Name == "" {
				repo.Name = n
			}
		}
	}

	if repo.Owner == "" || repo.Name == "" {
		return registry.RepoRef{}, false
	}
	return repo, true
}

// normalizePushBody returns the JSON document from a webhook body, tolerating
// both content types a GitHub webhook can be configured to send:
//
//   - application/json — the body already is the JSON document.
//   - application/x-www-form-urlencoded — GitHub sends "payload=<url-encoded
//     JSON>" instead, and a receiver that only json.Unmarshal's the raw body
//     silently fails to find the repository (the cause of "push had no
//     repository in payload").
//
// The HMAC signature is computed over the raw delivered bytes either way, so
// Verify is unaffected; only the bytes we hand to the JSON parser need
// unwrapping. Detection is by the "payload=" prefix — a JSON body always starts
// with "{", so the two never collide.
func normalizePushBody(body []byte) []byte {
	const formPrefix = "payload="
	if !bytes.HasPrefix(body, []byte(formPrefix)) {
		return body
	}
	decoded, err := url.QueryUnescape(string(body[len(formPrefix):]))
	if err != nil {
		return body
	}
	return []byte(decoded)
}
