// Package replicate does Harbor-style pull replication from ghcr.io.
//
// The steps are the same as in a Harbor GHCR adapter that supports wildcards:
//
//  1. Find the repositories. If the name filter is specific ("org/a" or
//     "org/{a,b}"), use the names directly. If the filter has wildcards,
//     list the packages of the owner with the GitHub Packages API. Then
//     apply the filter.
//  2. List the tags of each repository. Then apply the tag filter.
//  3. Find the destination path of each repository (Harbor flattening).
//  4. Copy each tag that has a different digest at the destination. Obey the
//     override setting.
package replicate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"golang.org/x/sync/errgroup"

	"github.com/pelotech/ghcr-replicator/internal/config"
	"github.com/pelotech/ghcr-replicator/internal/harbor/controller/replication/flow"
	"github.com/pelotech/ghcr-replicator/internal/harbor/pkg/reg/util"
)

// RepositoryLister lists the source repositories that a name filter with
// wildcards selects. githubcr.Lister implements it.
type RepositoryLister interface {
	ListRepositories(ctx context.Context, pattern string) ([]string, error)
}

// Options configures a Replicator.
type Options struct {
	Lister    RepositoryLister
	Keychain  authn.Keychain
	Transport http.RoundTripper // optional
	UserAgent string            // optional
	DryRun    bool
	Logger    *slog.Logger // optional
}

// Result is the summary of one rule run.
type Result struct {
	Rule string
	// Copied is the number of tags that the run wrote.
	Copied int
	// WouldCopy is the number of tags that a dry run did not write, but that a
	// real run would write.
	WouldCopy int
	// UpToDate is the number of tags that already had the source digest at the
	// destination.
	UpToDate int
	// Skipped is the number of tags that have a different digest at the
	// destination when override is false.
	Skipped int
	// Failed is the number of tags, repositories and rule steps with an error.
	Failed int
}

// Replicator runs replication rules.
type Replicator struct {
	cfg  *config.Config
	opts Options
	log  *slog.Logger
	// srcReg and dstReg are the parsed registries. Build each reference
	// from them. Do not parse "host/repo" strings: a host without "." or ":"
	// (for example "harbor") then becomes a Docker Hub repository.
	srcReg name.Registry
	dstReg name.Registry
}

// New returns a Replicator for cfg.
func New(cfg *config.Config, opts Options) (*Replicator, error) {
	if opts.Lister == nil {
		return nil, errors.New("replicate: Options.Lister is required")
	}
	if opts.Keychain == nil {
		opts.Keychain = authn.NewMultiKeychain()
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	r := &Replicator{cfg: cfg, opts: opts, log: log}
	var err error
	if r.srcReg, err = parseRegistry(cfg.Source.Registry, cfg.Source.Insecure); err != nil {
		return nil, fmt.Errorf("source registry: %w", err)
	}
	if r.dstReg, err = parseRegistry(cfg.Destination.Registry, cfg.Destination.Insecure); err != nil {
		return nil, fmt.Errorf("destination registry: %w", err)
	}
	allowHTTP := map[string]bool{}
	if cfg.Source.Insecure {
		allowHTTP[cfg.Source.Registry] = true
	}
	if cfg.Destination.Insecure {
		allowHTTP[cfg.Destination.Registry] = true
	}
	base := r.opts.Transport
	if base == nil {
		base = remote.DefaultTransport
	}
	r.opts.Transport = requireTLS{base: base, allowHTTP: allowHTTP}
	return r, nil
}

func (r *Replicator) remoteOpts(ctx context.Context) []remote.Option {
	o := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(r.opts.Keychain),
		remote.WithTransport(r.opts.Transport),
	}
	if r.opts.UserAgent != "" {
		o = append(o, remote.WithUserAgent(r.opts.UserAgent))
	}
	return o
}

// Run runs each rule in order. When an item fails, Run counts the
// failure in the results and continues. Run returns an
// error only if ctx is cancelled.
func (r *Replicator) Run(ctx context.Context, rules []config.Rule) ([]Result, error) {
	var results []Result
	for _, rule := range rules {
		results = append(results, r.runRule(ctx, rule))
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}

// parseRegistry parses a registry host. Unlike a full reference, the host is
// always the registry, also without "." or ":".
func parseRegistry(host string, insecure bool) (name.Registry, error) {
	var opts []name.Option
	if insecure {
		opts = append(opts, name.Insecure)
	}
	return name.NewRegistry(host, opts...)
}

type task struct {
	srcRepo, dstRepo, tag string
}

// splitConflicts removes the tasks whose destination is also the destination
// of another task. This can occur when flattening maps different source
// repositories to the same destination repository. The replicator must not
// copy these tasks, because the copies would overwrite each other.
// conflicts maps each destination "repo:tag" to its source "repo:tag" list.
func splitConflicts(tasks []task) (ok []task, conflicts map[string][]string) {
	byDst := map[string][]task{}
	for _, t := range tasks {
		dst := t.dstRepo + ":" + t.tag
		byDst[dst] = append(byDst[dst], t)
	}
	conflicts = map[string][]string{}
	for _, t := range tasks {
		dst := t.dstRepo + ":" + t.tag
		same := byDst[dst]
		if len(same) == 1 {
			ok = append(ok, t)
			continue
		}
		if _, done := conflicts[dst]; done {
			continue
		}
		for _, s := range same {
			conflicts[dst] = append(conflicts[dst], s.srcRepo+":"+s.tag)
		}
	}
	return ok, conflicts
}

// groupByDestination groups tasks by destination repository. The groups and
// the tasks in each group keep the order of tasks.
func groupByDestination(tasks []task) [][]task {
	var groups [][]task
	index := map[string]int{}
	for _, t := range tasks {
		i, ok := index[t.dstRepo]
		if !ok {
			i = len(groups)
			index[t.dstRepo] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], t)
	}
	return groups
}

type outcome int

const (
	copied outcome = iota
	wouldCopy
	upToDate
	skipped
	failed
)

func (r *Replicator) runRule(ctx context.Context, rule config.Rule) Result {
	log := r.log.With("rule", rule.Name)
	res := Result{Rule: rule.Name}

	repos, err := r.resolveRepositories(ctx, rule)
	if err != nil {
		log.Error("resolve repositories", "err", err)
		res.Failed++
		return res
	}
	log.Info("resolved repositories", "count", len(repos), "filter", rule.Filters.Name)

	var (
		mu    sync.Mutex
		tasks []task
	)
	g := new(errgroup.Group)
	g.SetLimit(r.cfg.Concurrency)
	for _, repo := range repos {
		g.Go(func() error {
			ts, err := r.planRepository(ctx, rule, repo)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Error("plan repository", "repository", repo, "err", err)
				res.Failed++
				return nil
			}
			tasks = append(tasks, ts...)
			return nil
		})
	}
	_ = g.Wait()

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].srcRepo != tasks[j].srcRepo {
			return tasks[i].srcRepo < tasks[j].srcRepo
		}
		return tasks[i].tag < tasks[j].tag
	})

	tasks, conflicts := splitConflicts(tasks)
	for dst, sources := range conflicts {
		log.Error("more than one source maps to the same destination; check the flattening level", "destination", dst, "sources", sources)
		res.Failed += len(sources)
	}

	syncTask := func(t task) {
		out, err := r.syncTag(ctx, t, rule.ShouldOverride())
		l := log.With("source", t.srcRepo+":"+t.tag, "destination", t.dstRepo+":"+t.tag)
		mu.Lock()
		defer mu.Unlock()
		switch out {
		case copied:
			res.Copied++
			l.Info("copied")
		case wouldCopy:
			res.WouldCopy++
			l.Info("would copy")
		case upToDate:
			res.UpToDate++
			l.Debug("up to date")
		case skipped:
			res.Skipped++
			l.Warn("destination tag differs and override is disabled, skipping")
		case failed:
			res.Failed++
			l.Error("sync failed", "err", err)
		}
	}

	// Copy the tags of one destination repository one at a time. Tags of one
	// image usually share layers. If two copies upload the same layer to the
	// same repository at the same time, Harbor can reject one of them
	// (BLOB_UPLOAD_INVALID). In sequence, a later tag finds the shared layers
	// in the destination and does not upload them again. Different
	// repositories copy in parallel.
	g = new(errgroup.Group)
	g.SetLimit(r.cfg.Concurrency)
	for _, group := range groupByDestination(tasks) {
		g.Go(func() error {
			for _, t := range group {
				syncTask(t)
			}
			return nil
		})
	}
	_ = g.Wait()

	log.Info("rule finished", "copied", res.Copied, "wouldCopy", res.WouldCopy, "upToDate", res.UpToDate, "skipped", res.Skipped, "failed", res.Failed, "dryRun", r.opts.DryRun)
	return res
}

// resolveRepositories returns the source repositories that the name filter of
// the rule selects. In Harbor, this is the start of FetchArtifacts in the
// GHCR adapter.
func (r *Replicator) resolveRepositories(ctx context.Context, rule config.Rule) ([]string, error) {
	if paths, ok := util.IsSpecificPath(rule.Filters.Name); ok {
		// Harbor does the same: it uses the names directly. If a repository
		// does not exist, the tag list step finds the problem.
		return paths, nil
	}
	repos, err := r.opts.Lister.ListRepositories(ctx, rule.Filters.Name)
	if err != nil {
		return nil, err
	}
	sort.Strings(repos)
	return repos, nil
}

// planRepository lists the tags of a repository. It returns a task for each
// tag that passes the tag filter. If the repository does not exist, it
// returns no tasks.
func (r *Replicator) planRepository(ctx context.Context, rule config.Rule, repo string) ([]task, error) {
	dstRepo, err := flow.ReplaceNamespace(repo, rule.DestNamespace, rule.FlattenLevel())
	if err != nil {
		return nil, err
	}
	tags, err := remote.List(r.srcReg.Repo(repo), r.remoteOpts(ctx)...)
	if err != nil {
		if isNotFound(err) {
			r.log.Warn("source repository not found, skipping", "rule", rule.Name, "repository", repo)
			return nil, nil
		}
		return nil, fmt.Errorf("list tags: %w", err)
	}
	var tasks []task
	for _, tag := range tags {
		keep, err := tagSelected(rule.Filters, tag)
		if err != nil {
			return nil, err
		}
		if keep {
			tasks = append(tasks, task{srcRepo: repo, dstRepo: dstRepo, tag: tag})
		}
	}
	return tasks, nil
}

// tagSelected returns true if tag passes the tag filter.
func tagSelected(f config.Filters, tag string) (bool, error) {
	if f.Tag == "" {
		return true, nil
	}
	m, err := util.Match(f.Tag, tag)
	if err != nil {
		return false, fmt.Errorf("tag filter %q: %w", f.Tag, err)
	}
	if f.TagDecoration == config.DecorationExcludes {
		return !m, nil
	}
	return m, nil
}

func (r *Replicator) syncTag(ctx context.Context, t task, override bool) (outcome, error) {
	src := r.srcReg.Repo(t.srcRepo).Tag(t.tag)
	dst := r.dstReg.Repo(t.dstRepo).Tag(t.tag)
	ro := r.remoteOpts(ctx)

	srcDesc, err := remote.Head(src, ro...)
	if err != nil {
		return failed, fmt.Errorf("head source: %w", err)
	}
	dstDesc, err := remote.Head(dst, ro...)
	switch {
	case err == nil && dstDesc.Digest == srcDesc.Digest:
		return upToDate, nil
	case err == nil && !override:
		return skipped, nil
	case err != nil && !isNotFound(err):
		return failed, fmt.Errorf("head destination: %w", err)
	}

	if r.opts.DryRun {
		return wouldCopy, nil
	}
	desc, err := remote.Get(src, ro...)
	if err != nil {
		return failed, fmt.Errorf("get source: %w", err)
	}
	if err := write(dst, desc, ro); err != nil {
		return failed, fmt.Errorf("write destination: %w", err)
	}
	return copied, nil
}

func write(dst name.Reference, desc *remote.Descriptor, ro []remote.Option) error {
	switch {
	case desc.MediaType.IsIndex():
		idx, err := desc.ImageIndex()
		if err != nil {
			return err
		}
		return remote.WriteIndex(dst, idx, ro...)
	case desc.MediaType.IsSchema1():
		return fmt.Errorf("docker schema1 manifests are not supported (%s)", desc.Digest)
	case desc.MediaType.IsImage():
		img, err := desc.Image()
		if err != nil {
			return err
		}
		return remote.Write(dst, img, ro...)
	default:
		return fmt.Errorf("unsupported manifest media type %q", desc.MediaType)
	}
}

func isNotFound(err error) bool {
	var te *transport.Error
	return errors.As(err, &te) && te.StatusCode == http.StatusNotFound
}
