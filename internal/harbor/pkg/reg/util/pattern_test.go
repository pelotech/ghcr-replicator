package util

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, str string
		want         bool
	}{
		{"", "anything/at/all", true},
		{"my-org/**", "my-org/apps/base/nginx-fips", true},
		{"my-org/**", "other/apps", false},
		// In doublestar v1, "*" and "**" do not cross "/". Only "/**" crosses "/".
		{"my-org/apps**", "my-org/apps-bundles", true},
		{"my-org/apps**", "my-org/apps/api", false},
		{"my-org/apps*", "my-org/apps", true},
		{"my-org/apps*", "my-org/apps/api", false},
		{"my-org/apps/**", "my-org/apps/api", true},
		{"my-org/apps/**", "my-org/apps/base/nginx-fips", true},
		{"my-org/apps/**", "my-org/apps", false},
		{"my-org/{apps*,apps/**}", "my-org/apps-bundles", true},
		{"my-org/{apps*,apps/**}", "my-org/apps/api", true},
		// Tag filters.
		{"{,v}[0-9]*.[0-9]*.[0-9]*", "1.271.1", true},
		{"{,v}[0-9]*.[0-9]*.[0-9]*", "v1.2.3", true},
		{"{,v}[0-9]*.[0-9]*.[0-9]*", "latest", false},
		{"{,v}[0-9]*.[0-9]*.[0-9]*", "sha256-abc.sig", false},
	}
	for _, tt := range tests {
		got, err := Match(tt.pattern, tt.str)
		if err != nil {
			t.Fatalf("Match(%q, %q) error: %v", tt.pattern, tt.str, err)
		}
		if got != tt.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.str, got, tt.want)
		}
	}
}

func TestIsSpecificPath(t *testing.T) {
	tests := []struct {
		path  string
		want  []string
		wantB bool
	}{
		{"", nil, false},
		{"library/hello-world", []string{"library/hello-world"}, true},
		{"library/**", nil, false},
		{"library/hello*", nil, false},
		{"library/{a,b}", []string{"library/a", "library/b"}, true},
		{"{x,y}/{a,b}", []string{"x/a", "x/b", "y/a", "y/b"}, true},
		{"library/pre{a,b}suf", []string{"library/preasuf", "library/prebsuf"}, true},
		{"library/{a,b}{c,d}", nil, false},
		{"library/a}b{", nil, false},
	}
	for _, tt := range tests {
		got, ok := IsSpecificPath(tt.path)
		if ok != tt.wantB || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("IsSpecificPath(%q) = %v, %v; want %v, %v", tt.path, got, ok, tt.want, tt.wantB)
		}
	}
}
