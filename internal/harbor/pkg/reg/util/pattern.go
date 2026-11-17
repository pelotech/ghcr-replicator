// Package util implements the Harbor replication filter rules.
//
// Match, IsSpecificPath and IsSpecificPathComponent come from goharbor/harbor
// src/pkg/reg/util/pattern.go (Apache-2.0, Copyright Project Harbor Authors).
// Thus the filters operate the same as in a Harbor replication rule.
//
// Do not upgrade github.com/bmatcuk/doublestar to v2 or later. Harbor uses
// v1.3.4. Later versions match patterns differently. For example, "*" and
// "**" treat "/" differently. Thus a filter can select different
// repositories than in Harbor. Upgrade only when Harbor upgrades.
package util

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar"
)

// Match returns true if str matches pattern. An empty pattern matches all
// strings. Harbor does the same.
func Match(pattern, str string) (bool, error) {
	if len(pattern) == 0 {
		return true, nil
	}
	return doublestar.Match(pattern, str)
}

// IsSpecificPath returns true if path identifies a fixed set of repositories.
// A specific path has no wildcards. Each path component has a maximum of one
// {a,b} group. If path is specific, the function also returns the set.
// For example, "library/{a,b}" gives [library/a library/b], true.
// "library/**" gives nil, false.
func IsSpecificPath(path string) ([]string, bool) {
	if len(path) == 0 {
		return nil, false
	}
	components := [][]string{}
	for component := range strings.SplitSeq(path, "/") {
		strs, ok := IsSpecificPathComponent(component)
		if !ok {
			return nil, false
		}
		components = append(components, strs)
	}

	result := []string{}
	for _, component := range components {
		result = combinationPathComponents(result, component)
	}
	return result, true
}

func combinationPathComponents(components1, components2 []string) []string {
	if len(components1) == 0 {
		return components2
	}
	if len(components2) == 0 {
		return components1
	}
	components := []string{}
	for _, component1 := range components1 {
		for _, component2 := range components2 {
			components = append(components, fmt.Sprintf("%s/%s", component1, component2))
		}
	}
	return components
}

// IsSpecificPathComponent returns true if one path component is a fixed
// string or one {a,b} group. It also returns the expanded values.
func IsSpecificPathComponent(component string) ([]string, bool) {
	if len(component) == 0 {
		return nil, false
	}
	// contains any of *?[\\]^
	if strings.ContainsAny(component, "*?[\\]^/") {
		return nil, false
	}
	// doesn't contain {},
	if !strings.ContainsAny(component, "{},") {
		return []string{component}, true
	}
	// support only one pair of {} currently
	if strings.Count(component, "{") > 1 {
		return nil, false
	}
	i := strings.Index(component, "{")
	if i == -1 {
		return nil, false
	}
	j := strings.LastIndex(component, "}")
	if j == -1 {
		return nil, false
	}
	if i > j {
		return nil, false
	}
	prefix := component[:i]
	suffix := ""
	if j+1 < len(component) {
		suffix = component[j+1:]
	}
	components := []string{}
	for str := range strings.SplitSeq(component[i+1:j], ",") {
		components = append(components, prefix+str+suffix)
	}
	return components, true
}
