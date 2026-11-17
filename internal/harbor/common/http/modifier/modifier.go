// Package modifier comes from goharbor/harbor src/common/http/modifier
// (Apache-2.0, Copyright Project Harbor Authors).
package modifier

import (
	"net/http"
)

// Modifier changes a request before the client sends it.
type Modifier interface {
	Modify(*http.Request) error
}
