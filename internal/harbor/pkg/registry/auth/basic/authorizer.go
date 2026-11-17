// Package basic comes from goharbor/harbor src/pkg/registry/auth/basic
// (Apache-2.0, Copyright Project Harbor Authors). In Harbor, NewAuthorizer
// returns lib.Authorizer. That interface has the same Modify method.
package basic

import (
	"net/http"

	"github.com/pelotech/ghcr-replicator/internal/harbor/common/http/modifier"
)

// NewAuthorizer returns a modifier that adds basic authentication to each
// request. If username is empty, the modifier adds nothing.
func NewAuthorizer(username, password string) modifier.Modifier {
	return &authorizer{
		username: username,
		password: password,
	}
}

type authorizer struct {
	username string
	password string
}

func (a *authorizer) Modify(req *http.Request) error {
	if len(a.username) > 0 {
		req.SetBasicAuth(a.username, a.password)
	}
	return nil
}
