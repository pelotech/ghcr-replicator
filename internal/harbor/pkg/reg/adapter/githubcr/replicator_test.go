// This file is not for Harbor. It tests replicator.go.

package githubcr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRepositoriesStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Each page links to a next page. The server cancels ctx while it
	// serves the first page.
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		cancel()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=%d>; rel="next"`, server.URL, r.URL.Path, n+1))
		writePackages(w, fmt.Sprintf("app-%d", n))
	}))
	defer server.Close()

	_, err := NewLister(server.URL, "", "", "").ListRepositories(ctx, "my-org/**")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), requests.Load(), "no page after the first should be requested")
}
