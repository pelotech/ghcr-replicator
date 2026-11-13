package replicate

import (
	"fmt"
	"net/http"
)

// requireTLS is a transport that refuses plain HTTP requests, except to the
// hosts in allowHTTP. go-containerregistry selects HTTP by itself for some
// hosts, for example private IP addresses and localhost. Thus "insecure:
// false" alone does not make sure that credentials go over TLS.
type requireTLS struct {
	base      http.RoundTripper
	allowHTTP map[string]bool
}

func (t requireTLS) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" && !t.allowHTTP[req.URL.Host] {
		return nil, fmt.Errorf("refusing a plain HTTP request to %s: set insecure: true for this registry to allow it", req.URL.Host)
	}
	return t.base.RoundTrip(req)
}
