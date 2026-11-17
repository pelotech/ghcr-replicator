// Package http is a small part of goharbor/harbor src/common/http
// (Apache-2.0, Copyright Project Harbor Authors). It has only the parts that
// the githubcr code uses. The names and the behavior are the same as in
// Harbor. Thus the githubcr code can move to Harbor without changes.
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/pelotech/ghcr-replicator/internal/harbor/common/http/modifier"
	"github.com/pelotech/ghcr-replicator/internal/harbor/lib"
)

// Client sends HTTP requests. Each modifier changes the request before the
// client sends it, for example to add credentials.
type Client struct {
	modifiers []modifier.Modifier
	client    *http.Client
}

// NewClient returns a Client. If c is nil, NewClient uses a new http.Client.
func NewClient(c *http.Client, modifiers ...modifier.Modifier) *Client {
	client := &Client{
		client: c,
	}
	if client.client == nil {
		client.client = &http.Client{}
	}
	if len(modifiers) > 0 {
		client.modifiers = modifiers
	}
	return client
}

// Do applies the modifiers to req and sends it.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	for _, modifier := range c.modifiers {
		if err := modifier.Modify(req); err != nil {
			return nil, err
		}
	}

	return c.client.Do(req)
}

// Error contains the HTTP status code and the message of a failed request.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error returns the error text.
func (e *Error) Error() string {
	return fmt.Sprintf("http error: code %d, message %s", e.Code, e.Message)
}

// SameOrigin returns true if a and b have the same scheme, host and port.
func SameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		sameHost(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

// sameHost compares host names. The comparison ignores case, but an IPv6 zone
// identifier must be the same.
func sameHost(a, b string) bool {
	aHost, aZone, _ := strings.Cut(a, "%")
	bHost, bZone, _ := strings.Cut(b, "%")
	return strings.EqualFold(aHost, bHost) && aZone == bZone
}

// effectivePort returns the port of u. If u has no port, it returns the
// default port of the scheme.
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

// GetAndIteratePagination iterates the pagination header and returns all resources
// The parameter "v" must be a pointer to a slice
func (c *Client) GetAndIteratePagination(endpoint string, v any) error {
	url, err := url.Parse(endpoint)
	if err != nil {
		return err
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer {
		return errors.New("v should be a pointer to a slice")
	}
	elemType := rv.Elem().Type()
	if elemType.Kind() != reflect.Slice {
		return errors.New("v should be a pointer to a slice")
	}

	resources := reflect.Indirect(reflect.New(elemType))
	for len(endpoint) > 0 {
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return &Error{
				Code:    resp.StatusCode,
				Message: string(data),
			}
		}

		res := reflect.New(elemType)
		if err = json.Unmarshal(data, res.Interface()); err != nil {
			return err
		}
		resources = reflect.AppendSlice(resources, reflect.Indirect(res))

		endpoint = ""
		links := lib.ParseLinks(resp.Header.Get("Link"))
		for _, link := range links {
			if link.Rel == "next" {
				next, err := resolveNextLink(url, link.URL)
				if err != nil {
					return err
				}
				// encode the query parameters to avoid bad request
				// e.g. ?q=name={p1 p2 p3} need to be encoded to ?q=name%3D%7Bp1+p2+p3%7D
				next.RawQuery = next.Query().Encode()
				url = next
				endpoint = next.String()
				break
			}
		}
	}
	rv.Elem().Set(resources)
	return nil
}

// resolveNextLink resolves an RFC 5988 Link rel="next" target against the base URL of
// the current page and enforces that pagination never leaves the base URL's origin.
//
// The Link value is attacker-controlled (it comes verbatim from an upstream registry's
// response header during replication). The previous implementation built the next URL by
// string-concatenating scheme+host+link and re-parsing it, so a value such as
// "@evil.example/x" produced "scheme://host@evil.example/x", which net/url re-parses with
// evil.example as the network host and host as userinfo. Because the client re-runs its
// auth modifiers on every request, the configured registry credentials were then sent to
// that attacker-chosen host (SSRF plus credential disclosure).
//
// Resolving the link as a proper URL reference and requiring the same origin keeps
// legitimate relative pagination working while failing closed on any cross-origin target.
func resolveNextLink(base *url.URL, link string) (*url.URL, error) {
	ref, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	// userinfo has no legitimate use in a pagination link and is the vector that turns a
	// same-origin-looking value into a cross-origin host.
	if ref.User != nil {
		return nil, fmt.Errorf("pagination Link must not contain userinfo: %q", link)
	}
	next := base.ResolveReference(ref)
	if !SameOrigin(base, next) {
		return nil, fmt.Errorf("pagination Link %q resolves to a different origin than %q", link, base.Redacted())
	}
	return next, nil
}
