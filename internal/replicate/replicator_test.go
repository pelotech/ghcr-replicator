package replicate

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/pelotech/ghcr-replicator/internal/config"
	"github.com/pelotech/ghcr-replicator/internal/harbor/pkg/reg/adapter/githubcr"
	"github.com/pelotech/ghcr-replicator/internal/harbor/pkg/reg/util"
)

type fakeLister struct {
	mu    sync.Mutex
	repos map[string][]string
	err   error
	calls []string
}

// ListRepositories works like githubcr.Lister. It lists the repositories of
// each owner in pattern and returns the repositories that match pattern.
func (f *fakeLister) ListRepositories(_ context.Context, pattern string) ([]string, error) {
	owners, err := githubcr.Owners(pattern)
	if err != nil {
		return nil, err
	}
	var matched []string
	for _, owner := range owners {
		repos, err := f.listOwner(owner)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			if ok, _ := util.Match(pattern, repo); ok {
				matched = append(matched, repo)
			}
		}
	}
	return matched, nil
}

func (f *fakeLister) listOwner(owner string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, owner)
	if f.err != nil {
		return nil, f.err
	}
	return f.repos[owner], nil
}

type env struct {
	t        *testing.T
	src, dst string // registry hosts
	lister   *fakeLister
}

func newEnv(t *testing.T) *env {
	t.Helper()
	src := httptest.NewServer(newRegistry())
	dst := httptest.NewServer(newRegistry())
	t.Cleanup(src.Close)
	t.Cleanup(dst.Close)
	return &env{
		t:      t,
		src:    strings.TrimPrefix(src.URL, "http://"),
		dst:    strings.TrimPrefix(dst.URL, "http://"),
		lister: &fakeLister{repos: map[string][]string{}},
	}
}

func (e *env) pushImage(ref string) v1.Hash {
	e.t.Helper()
	img, err := random.Image(256, 2)
	if err != nil {
		e.t.Fatal(err)
	}
	tag, err := name.NewTag(e.src+"/"+ref, name.Insecure)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := remote.Write(tag, img); err != nil {
		e.t.Fatal(err)
	}
	d, err := img.Digest()
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *env) pushIndex(ref string) v1.Hash {
	e.t.Helper()
	idx, err := random.Index(128, 1, 2)
	if err != nil {
		e.t.Fatal(err)
	}
	tag, err := name.NewTag(e.src+"/"+ref, name.Insecure)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := remote.WriteIndex(tag, idx); err != nil {
		e.t.Fatal(err)
	}
	d, err := idx.Digest()
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

// dstDigest returns the destination digest of ref. If ref does not exist, it returns "".
func (e *env) dstDigest(ref string) string {
	e.t.Helper()
	tag, err := name.NewTag(e.dst+"/"+ref, name.Insecure)
	if err != nil {
		e.t.Fatal(err)
	}
	d, err := remote.Head(tag)
	if err != nil {
		if isNotFound(err) {
			return ""
		}
		e.t.Fatal(err)
	}
	return d.Digest.String()
}

func (e *env) config(rules ...config.Rule) *config.Config {
	for i := range rules {
		if rules[i].Filters.TagDecoration == "" {
			rules[i].Filters.TagDecoration = config.DecorationMatches
		}
	}
	return &config.Config{
		Source:      config.Source{Registry: e.src, Insecure: true},
		Destination: config.Destination{Registry: e.dst, Insecure: true},
		Concurrency: 4,
		Rules:       rules,
	}
}

func (e *env) run(cfg *config.Config, dryRun bool) []Result {
	e.t.Helper()
	r, err := New(cfg, Options{Lister: e.lister, DryRun: dryRun})
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := r.Run(context.Background(), cfg.Rules)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func newRegistry() http.Handler {
	return registry.New(registry.Logger(log.New(io.Discard, "", 0)))
}

func ptr[T any](v T) *T { return &v }

func assertResult(t *testing.T, got Result, want Result) {
	t.Helper()
	if got != want {
		t.Errorf("result = %+v, want %+v", got, want)
	}
}

const semver = "{,v}[0-9]*.[0-9]*.[0-9]*"

func appsRule() config.Rule {
	return config.Rule{
		Name:                 "apps",
		Filters:              config.Filters{Name: "my-org/apps/**", Tag: semver},
		DestNamespace:        "apps",
		DestNamespaceReplace: ptr(2),
	}
}

func TestWildcardRuleCopiesAndIsIdempotent(t *testing.T) {
	e := newEnv(t)
	api1 := e.pushImage("my-org/apps/api:1.0.0")
	api2 := e.pushImage("my-org/apps/api:v1.0.1")
	e.pushImage("my-org/apps/api:latest")
	web := e.pushIndex("my-org/apps/base/web:2.0.0")
	e.pushImage("my-org/apps-bundle:1.0.0") // apps/** does not match it
	e.pushImage("my-org/other/x:1.0.0")     // does not match
	e.lister.repos["my-org"] = []string{
		"my-org/apps-bundle",
		"my-org/apps/api",
		"my-org/apps/base/web",
		"my-org/apps/ghost", // The API lists it, but it has no images. Skip it, do not fail.
		"my-org/other/x",
	}
	cfg := e.config(appsRule())

	res := e.run(cfg, false)
	if len(res) != 1 {
		t.Fatalf("got %d results", len(res))
	}
	assertResult(t, res[0], Result{Rule: "apps", Copied: 3})

	for ref, want := range map[string]string{
		"apps/api:1.0.0":         api1.String(),
		"apps/api:v1.0.1":        api2.String(),
		"apps/base/web:2.0.0":    web.String(),
		"apps/api:latest":        "",
		"apps/apps-bundle:1.0.0": "",
		"apps/x:1.0.0":           "",
	} {
		if got := e.dstDigest(ref); got != want {
			t.Errorf("dst %s digest = %q, want %q", ref, got, want)
		}
	}
	if len(e.lister.calls) != 1 || e.lister.calls[0] != "my-org" {
		t.Errorf("lister calls = %v", e.lister.calls)
	}

	// The second run has nothing to do.
	assertResult(t, e.run(cfg, false)[0], Result{Rule: "apps", UpToDate: 3})
}

func TestOverride(t *testing.T) {
	e := newEnv(t)
	e.pushImage("my-org/apps/api:1.0.0")
	e.lister.repos["my-org"] = []string{"my-org/apps/api"}
	rule := appsRule()
	e.run(e.config(rule), false)

	// The source tag moves to a new digest.
	moved := e.pushImage("my-org/apps/api:1.0.0")

	rule.Override = ptr(false)
	assertResult(t, e.run(e.config(rule), false)[0], Result{Rule: "apps", Skipped: 1})
	if got := e.dstDigest("apps/api:1.0.0"); got == moved.String() {
		t.Fatal("override=false still overwrote the destination tag")
	}

	rule.Override = ptr(true)
	assertResult(t, e.run(e.config(rule), false)[0], Result{Rule: "apps", Copied: 1})
	if got := e.dstDigest("apps/api:1.0.0"); got != moved.String() {
		t.Fatalf("override=true: dst digest = %s, want %s", got, moved)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.pushImage("my-org/apps/api:1.0.0")
	e.lister.repos["my-org"] = []string{"my-org/apps/api"}

	assertResult(t, e.run(e.config(appsRule()), true)[0], Result{Rule: "apps", WouldCopy: 1})
	if got := e.dstDigest("apps/api:1.0.0"); got != "" {
		t.Fatalf("dry run wrote %s", got)
	}
}

func TestSplitConflicts(t *testing.T) {
	tests := []struct {
		name          string
		tasks         []task
		wantOK        []task
		wantConflicts map[string][]string
	}{
		{
			name: "no conflict",
			tasks: []task{
				{srcRepo: "o/one/app", dstRepo: "m/one/app", tag: "1"},
				{srcRepo: "o/two/app", dstRepo: "m/two/app", tag: "1"},
			},
			wantOK: []task{
				{srcRepo: "o/one/app", dstRepo: "m/one/app", tag: "1"},
				{srcRepo: "o/two/app", dstRepo: "m/two/app", tag: "1"},
			},
			wantConflicts: map[string][]string{},
		},
		{
			name: "two sources, one destination",
			tasks: []task{
				{srcRepo: "o/one/app", dstRepo: "m/app", tag: "1"},
				{srcRepo: "o/two/app", dstRepo: "m/app", tag: "1"},
			},
			wantOK:        nil,
			wantConflicts: map[string][]string{"m/app:1": {"o/one/app:1", "o/two/app:1"}},
		},
		{
			name: "same repository, different tags",
			tasks: []task{
				{srcRepo: "o/one/app", dstRepo: "m/app", tag: "1"},
				{srcRepo: "o/two/app", dstRepo: "m/app", tag: "2"},
			},
			wantOK: []task{
				{srcRepo: "o/one/app", dstRepo: "m/app", tag: "1"},
				{srcRepo: "o/two/app", dstRepo: "m/app", tag: "2"},
			},
			wantConflicts: map[string][]string{},
		},
		{
			name: "only the conflicting tag is removed",
			tasks: []task{
				{srcRepo: "o/one/app", dstRepo: "m/app", tag: "1"},
				{srcRepo: "o/one/app", dstRepo: "m/app", tag: "2"},
				{srcRepo: "o/two/app", dstRepo: "m/app", tag: "1"},
			},
			wantOK:        []task{{srcRepo: "o/one/app", dstRepo: "m/app", tag: "2"}},
			wantConflicts: map[string][]string{"m/app:1": {"o/one/app:1", "o/two/app:1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, conflicts := splitConflicts(tt.tasks)
			if !reflect.DeepEqual(ok, tt.wantOK) {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(conflicts, tt.wantConflicts) {
				t.Errorf("conflicts = %v, want %v", conflicts, tt.wantConflicts)
			}
		})
	}
}

func TestGroupByDestination(t *testing.T) {
	tasks := []task{
		{srcRepo: "o/a", dstRepo: "m/a", tag: "1"},
		{srcRepo: "o/a", dstRepo: "m/a", tag: "2"},
		{srcRepo: "o/b", dstRepo: "m/b", tag: "1"},
		{srcRepo: "o/one/c", dstRepo: "m/c", tag: "1"},
		{srcRepo: "o/two/c", dstRepo: "m/c", tag: "2"},
	}
	want := [][]task{
		{{srcRepo: "o/a", dstRepo: "m/a", tag: "1"}, {srcRepo: "o/a", dstRepo: "m/a", tag: "2"}},
		{{srcRepo: "o/b", dstRepo: "m/b", tag: "1"}},
		{{srcRepo: "o/one/c", dstRepo: "m/c", tag: "1"}, {srcRepo: "o/two/c", dstRepo: "m/c", tag: "2"}},
	}
	if got := groupByDestination(tasks); !reflect.DeepEqual(got, want) {
		t.Errorf("groupByDestination = %v, want %v", got, want)
	}
}

func TestFlatteningConflictCopiesNothing(t *testing.T) {
	e := newEnv(t)
	e.pushImage("o/one/app:latest")
	e.pushImage("o/two/app:latest")
	e.lister.repos["o"] = []string{"o/one/app", "o/two/app"}
	rule := config.Rule{
		Name:                 "flat",
		Filters:              config.Filters{Name: "o/**"},
		DestNamespace:        "mirror",
		DestNamespaceReplace: ptr(-1),
		Override:             ptr(false),
	}

	assertResult(t, e.run(e.config(rule), false)[0], Result{Rule: "flat", Failed: 2})
	if got := e.dstDigest("mirror/app:latest"); got != "" {
		t.Fatalf("conflicting tag was written: %s", got)
	}
}

func TestSpecificNameFilterSkipsListing(t *testing.T) {
	e := newEnv(t)
	e.pushImage("my-org/apps/api:1.0.0")
	e.pushImage("my-org/apps/api:latest")
	e.lister.err = errors.New("lister must not be called for a specific name filter")

	rule := config.Rule{
		Name: "explicit",
		Filters: config.Filters{
			Name:          "my-org/apps/{api,missing}",
			Tag:           "latest",
			TagDecoration: config.DecorationExcludes,
		},
		DestNamespace:        "mirror",
		DestNamespaceReplace: ptr(-1),
	}
	assertResult(t, e.run(e.config(rule), false)[0], Result{Rule: "explicit", Copied: 1})
	if e.dstDigest("mirror/api:1.0.0") == "" || e.dstDigest("mirror/api:latest") != "" {
		t.Error("excludes tag filter not applied")
	}
	if len(e.lister.calls) != 0 {
		t.Errorf("lister called: %v", e.lister.calls)
	}
}

func TestFailuresAreCountedNotFatal(t *testing.T) {
	e := newEnv(t)
	e.pushImage("my-org/a:1.0.0")
	e.lister.repos["my-org"] = []string{"my-org/a"}

	listerFails := config.Rule{Name: "lister-fails", Filters: config.Filters{Name: "broken/**"}}
	badFlatten := config.Rule{
		Name:                 "bad-flatten",
		Filters:              config.Filters{Name: "my-org/*"},
		DestNamespace:        "ns",
		DestNamespaceReplace: ptr(5),
	}
	ok := config.Rule{Name: "ok", Filters: config.Filters{Name: "my-org/*"}, DestNamespace: "ns"}

	e.lister.repos["broken"] = nil
	failing := &erroringLister{fakeLister: e.lister, failOwner: "broken"}
	cfg := e.config(listerFails, badFlatten, ok)
	r, err := New(cfg, Options{Lister: failing})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), cfg.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("want 3 results, got %+v", res)
	}
	assertResult(t, res[0], Result{Rule: "lister-fails", Failed: 1})
	assertResult(t, res[1], Result{Rule: "bad-flatten", Failed: 1})
	assertResult(t, res[2], Result{Rule: "ok", Copied: 1})
	if e.dstDigest("ns/a:1.0.0") == "" {
		t.Error("ok rule did not copy")
	}
}

type erroringLister struct {
	*fakeLister
	failOwner string
}

func (l *erroringLister) ListRepositories(ctx context.Context, pattern string) ([]string, error) {
	if strings.HasPrefix(pattern, l.failOwner+"/") {
		return nil, errors.New("boom")
	}
	return l.fakeLister.ListRepositories(ctx, pattern)
}

func TestDestinationCredentialsAreUsed(t *testing.T) {
	src := httptest.NewServer(newRegistry())
	t.Cleanup(src.Close)
	inner := newRegistry()
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "robot$replicator" || p != "pw" {
			w.Header().Set("WWW-Authenticate", `Basic realm="harbor"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(dst.Close)

	e := &env{
		t:      t,
		src:    strings.TrimPrefix(src.URL, "http://"),
		dst:    strings.TrimPrefix(dst.URL, "http://"),
		lister: &fakeLister{repos: map[string][]string{"o": {"o/app"}}},
	}
	e.pushImage("o/app:1.0.0")
	cfg := e.config(config.Rule{Name: "auth", Filters: config.Filters{Name: "o/**"}, DestNamespace: "p"})

	// Without credentials, the copy fails.
	r, _ := New(cfg, Options{Lister: e.lister})
	res, _ := r.Run(context.Background(), cfg.Rules)
	assertResult(t, res[0], Result{Rule: "auth", Failed: 1})

	// With credentials, the copy succeeds.
	kc := Keychain{}
	kc.Add(e.dst, "robot$replicator", "pw")
	r, _ = New(cfg, Options{Lister: e.lister, Keychain: kc})
	res, _ = r.Run(context.Background(), cfg.Rules)
	assertResult(t, res[0], Result{Rule: "auth", Copied: 1})
}

// go-containerregistry uses plain HTTP for loopback and private IP addresses
// by itself. With insecure: false, the replicator must not send any request
// to such a registry.
func TestNoPlainHTTPWithoutInsecure(t *testing.T) {
	e := newEnv(t)
	e.pushImage("my-org/apps/api:1.0.0")
	e.lister.repos["my-org"] = []string{"my-org/apps/api"}

	var dstRequests atomic.Int32
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dstRequests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(dst.Close)

	cfg := e.config(appsRule())
	cfg.Destination = config.Destination{Registry: strings.TrimPrefix(dst.URL, "http://")}
	r, err := New(cfg, Options{Lister: e.lister})
	if err != nil {
		t.Fatal(err)
	}
	res, _ := r.Run(context.Background(), cfg.Rules)

	assertResult(t, res[0], Result{Rule: "apps", Failed: 1})
	if n := dstRequests.Load(); n != 0 {
		t.Errorf("destination got %d plain HTTP requests, want 0", n)
	}
}

func TestKeychain(t *testing.T) {
	kc := Keychain{}
	kc.Add("ghcr.io", "", "") // Add ignores empty credentials.
	kc.Add("harbor.example.com", "u", "p")

	for host, wantAnon := range map[string]bool{"ghcr.io": true, "harbor.example.com": false, "other.io": true} {
		reg, _ := name.NewRegistry(host)
		a, err := kc.Resolve(reg)
		if err != nil {
			t.Fatal(err)
		}
		if (a == authn.Anonymous) != wantAnon {
			t.Errorf("%s: anonymous = %v, want %v", host, a == authn.Anonymous, wantAnon)
		}
	}
}

func TestNewRequiresLister(t *testing.T) {
	if _, err := New(&config.Config{}, Options{}); err == nil {
		t.Fatal("expected error")
	}
}

// A registry host without "." or ":" must stay the registry. It must not
// become a Docker Hub repository.
func TestBareRegistryHostIsNotDockerHub(t *testing.T) {
	cfg := &config.Config{
		Source:      config.Source{Registry: "ghcr"},
		Destination: config.Destination{Registry: "harbor"},
		Concurrency: 1,
	}
	r, err := New(cfg, Options{Lister: &fakeLister{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.srcReg.Repo("o/app").Tag("1").Context().RegistryStr(); got != "ghcr" {
		t.Errorf("source registry = %q, want ghcr", got)
	}
	if got := r.dstReg.Repo("mirror/app").Tag("1").Context().RegistryStr(); got != "harbor" {
		t.Errorf("destination registry = %q, want harbor", got)
	}
}
