// This file is not for Harbor. It connects the upstream candidate code in
// packages.go to the replicator.

package githubcr

import (
	"context"
	"net/http"
	"strings"

	common_http "github.com/pelotech/ghcr-replicator/internal/harbor/common/http"
	"github.com/pelotech/ghcr-replicator/internal/harbor/common/http/modifier"
	"github.com/pelotech/ghcr-replicator/internal/harbor/pkg/registry/auth/basic"
)

// adapter has the adapter fields that packages.go uses. In Harbor, the
// adapter struct in adapter.go has the same client field. The apiURL field
// is new in Harbor too: newAdapter sets it to githubAPIURL.
type adapter struct {
	client *common_http.Client
	apiURL string
}

// Lister lists the GHCR repositories that a repository name filter selects.
type Lister struct {
	apiURL    string
	client    *http.Client
	modifiers []modifier.Modifier
}

// NewLister returns a Lister. apiURL is the GitHub REST API URL. If it is
// empty, NewLister uses https://api.github.com. If username is empty, the
// requests use anonymous access. If client is nil, NewLister uses a new
// http.Client.
func NewLister(apiURL, username, token, userAgent string, client *http.Client) *Lister {
	if apiURL == "" {
		apiURL = githubAPIURL
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Lister{
		apiURL: strings.TrimRight(apiURL, "/"),
		client: client,
		// The client sends the token as basic auth, not as a bearer token,
		// because the client in the Harbor GHCR adapter does the same. GitHub
		// accepts a token as the basic auth password.
		modifiers: []modifier.Modifier{basic.NewAuthorizer(username, token), userAgentModifier(userAgent)},
	}
}

// ListRepositories returns the repositories that match pattern. The first
// path component of pattern must be a fixed owner name or one {a,b} group.
// If ctx is cancelled, the current request stops and no more pages are
// requested.
func (l *Lister) ListRepositories(ctx context.Context, pattern string) ([]string, error) {
	// The code in packages.go does not use a context, as in Harbor. Thus the
	// client adds ctx to each request.
	client := *l.client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = contextTransport{ctx: ctx, base: base}

	a := &adapter{
		client: common_http.NewClient(&client, l.modifiers...),
		apiURL: l.apiURL,
	}
	return a.listRepositories(pattern)
}

// Owners returns the GitHub owners that pattern can match. It returns an
// error if the owner in pattern has wildcards. Use it to validate a config.
func Owners(pattern string) ([]string, error) {
	return ownersOf(pattern)
}

// contextTransport adds ctx to each request.
type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req.WithContext(t.ctx))
}

// userAgentModifier sets the User-Agent header. If it is empty, it does
// nothing.
type userAgentModifier string

func (u userAgentModifier) Modify(req *http.Request) error {
	if u != "" {
		req.Header.Set("User-Agent", string(u))
	}
	return nil
}
