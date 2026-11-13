package flow

import "testing"

func TestReplaceNamespace(t *testing.T) {
	tests := []struct {
		repo, ns string
		replace  int
		want     string
		wantErr  bool
	}{
		// Usual flattening setups.
		{"my-org/apps/api", "apps", 2, "apps/api", false},
		{"my-org/app-a", "apps", 1, "apps/app-a", false},
		{"my-org/content/aus/page", "content/aus", 3, "content/aus/page", false},
		// Cases similar to the cases in Harbor stage_test.go.
		{"library/hello-world", "", 0, "library/hello-world", false},
		{"library/hello-world", "dest", -1, "dest/hello-world", false},
		{"a/b/c/d", "dest", -1, "dest/d", false},
		{"a/b/c/d", "dest", 0, "dest/a/b/c/d", false},
		{"a/b/c/d", "dest", 1, "dest/b/c/d", false},
		{"a/b/c/d", "dest", 3, "dest/d", false},
		{"a/b/c/d", "dest", 4, "", true},
		{"hello-world", "dest", 0, "dest/hello-world", false},
		{"hello-world", "dest", 1, "", true},
		{"a/b", "multi/level", 1, "multi/level/b", false},
	}
	for _, tt := range tests {
		got, err := ReplaceNamespace(tt.repo, tt.ns, tt.replace)
		if (err != nil) != tt.wantErr {
			t.Errorf("ReplaceNamespace(%q, %q, %d) error = %v, wantErr %v", tt.repo, tt.ns, tt.replace, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ReplaceNamespace(%q, %q, %d) = %q, want %q", tt.repo, tt.ns, tt.replace, got, tt.want)
		}
	}
}
