// Package lib is a small part of goharbor/harbor src/lib (Apache-2.0,
// Copyright Project Harbor Authors). It has only the Link header parser.
package lib

import (
	"strings"
)

// Link is one link in an HTTP Link header.
type Link struct {
	URL   string
	Rel   string
	Attrs map[string]string
}

// Links is a list of links.
type Links []*Link

// ParseLinks parses an HTTP Link header. For example:
// <http://example.com/TheBook/chapter2>; rel="previous"; title="previous chapter" , <http://example.com/TheBook/chapter4>; rel="next"; title="next chapter"
func ParseLinks(str string) Links {
	var links Links
	for lk := range strings.SplitSeq(str, ",") {
		link := &Link{
			Attrs: map[string]string{},
		}
		for attr := range strings.SplitSeq(lk, ";") {
			attr = strings.TrimSpace(attr)
			if len(attr) == 0 {
				continue
			}
			if attr[0] == '<' && attr[len(attr)-1] == '>' {
				link.URL = attr[1 : len(attr)-1]
				continue
			}

			parts := strings.SplitN(attr, "=", 2)
			key := parts[0]
			value := ""
			if len(parts) == 2 {
				value = strings.Trim(parts[1], `"`)
			}
			if key == "rel" {
				link.Rel = value
			} else {
				link.Attrs[key] = value
			}
		}
		if len(link.URL) == 0 {
			continue
		}
		links = append(links, link)
	}
	return links
}
