package prdata

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParseRepo(t *testing.T) {
	tests := []struct {
		in      string
		want    Repo
		wantErr bool
	}{
		{"owner/name", Repo{"owner", "name"}, false},
		{"tokio-rs/tokio", Repo{"tokio-rs", "tokio"}, false},
		{"owner", Repo{}, true},
		{"a/b/c", Repo{}, true},
		{"/name", Repo{}, true},
		{"", Repo{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseRepo(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("ParseRepo(%q) = (%v, %v), want (%v, err=%v)", tt.in, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestRepoFromRemoteURL(t *testing.T) {
	tests := []struct {
		url     string
		want    Repo
		wantErr bool
	}{
		{"https://github.com/NVIDIA/OpenShell.git", Repo{"NVIDIA", "OpenShell"}, false},
		{"https://github.com/NVIDIA/OpenShell", Repo{"NVIDIA", "OpenShell"}, false},
		{"git@github.com:caddyserver/caddy.git", Repo{"caddyserver", "caddy"}, false},
		{"ssh://git@github.com/apache/commons-lang.git", Repo{"apache", "commons-lang"}, false},
		{"https://gitlab.com/a/b.git", Repo{}, true},
		{"", Repo{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, err := RepoFromRemoteURL(tt.url)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("RepoFromRemoteURL(%q) = (%v, %v), want (%v, err=%v)", tt.url, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestDecodePage(t *testing.T) {
	body := `{"data":{"rateLimit":{"remaining":4900},"repository":{"pullRequests":{
	  "pageInfo":{"hasNextPage":true,"endCursor":"CUR"},
	  "nodes":[{"number":12,"title":"fix: x","mergedAt":"2026-05-02T10:00:00Z","updatedAt":"2026-05-03T10:00:00Z",
	    "mergeCommit":{"oid":"abc"},"author":{"login":"alice"},
	    "labels":{"nodes":[{"name":"bug"}]},
	    "closingIssuesReferences":{"nodes":[{"number":7,"labels":{"nodes":[{"name":"security"}]}}]},
	    "reviews":{"totalCount":4,"nodes":[
	      {"state":"APPROVED","author":{"login":"bob"}},
	      {"state":"APPROVED","author":{"login":"bob"}},
	      {"state":"COMMENTED","author":{"login":"carol"}},
	      {"state":"APPROVED","author":{"login":"dave"}}]}},
	   {"number":13,"title":"ghost","mergedAt":"2026-05-02T10:00:00Z","updatedAt":"2026-05-02T10:00:00Z",
	    "mergeCommit":null,"author":null,"labels":{"nodes":[]},"closingIssuesReferences":{"nodes":[]},
	    "reviews":{"totalCount":0,"nodes":[]}}]}}}}`
	page, err := decodePage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if page.EndCursor != "CUR" || !page.HasNextPage || page.RateRemaining != 4900 || len(page.PRs) != 2 {
		t.Fatalf("page = %+v", page)
	}
	want := PR{
		Number: 12, Title: "fix: x",
		MergedAt:    time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
		MergeCommit: "abc", Author: "alice", Labels: []string{"bug"},
		ClosingIssues: []int{7}, ClosingIssueLabels: []string{"security"},
		Reviews: 4, Approvers: 2,
	}
	if !reflect.DeepEqual(page.PRs[0], want) {
		t.Errorf("PR = %+v\nwant %+v", page.PRs[0], want)
	}
	if page.PRs[1].Author != "" || page.PRs[1].MergeCommit != "" {
		t.Errorf("null author/mergeCommit should decode as empty: %+v", page.PRs[1])
	}

	if _, err := decodePage([]byte(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`)); !errors.Is(err, ErrRateLimited) {
		t.Errorf("rate-limit GraphQL error = %v, want ErrRateLimited", err)
	}
	if _, err := decodePage([]byte(`{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`)); err == nil || errors.Is(err, ErrRateLimited) {
		t.Errorf("not-found GraphQL error = %v, want a plain error", err)
	}
	if _, err := decodePage([]byte(`not json`)); err == nil {
		t.Error("decodePage accepted invalid JSON")
	}
}

// fakeClient serves pages of PRs ordered by update time, newest first.
type fakeClient struct {
	pages     []Page
	errAt     int // page index that returns err (-1 for none)
	err       error
	calls     int
	cursorsIn []string
}

func (f *fakeClient) MergedPRsPage(_ context.Context, _ Repo, cursor string) (Page, error) {
	f.cursorsIn = append(f.cursorsIn, cursor)
	i := f.calls
	f.calls++
	if i == f.errAt {
		return Page{}, f.err
	}
	if i >= len(f.pages) {
		return Page{}, fmt.Errorf("no page %d", i)
	}
	return f.pages[i], nil
}

func day(d int) time.Time { return time.Date(2026, 5, d, 12, 0, 0, 0, time.UTC) }

func pr(n, mergedDay, updatedDay int) PR {
	return PR{Number: n, MergedAt: day(mergedDay), UpdatedAt: day(updatedDay)}
}

func TestFetch(t *testing.T) {
	repo := Repo{"o", "n"}
	pages := func() []Page {
		return []Page{
			{PRs: []PR{pr(5, 20, 21), pr(4, 2, 19)}, HasNextPage: true, EndCursor: "c1", RateRemaining: 4000},
			{PRs: []PR{pr(3, 15, 15), pr(2, 9, 9)}, HasNextPage: true, EndCursor: "c2", RateRemaining: 3999},
			{PRs: []PR{pr(1, 3, 3)}, HasNextPage: false, RateRemaining: 3998},
		}
	}
	base := FetchOptions{Since: day(8), Now: day(25), Max: 100, MinRemaining: 50}

	t.Run("pages until updates are older than the window", func(t *testing.T) {
		c := &fakeClient{pages: pages(), errAt: -1}
		opts := base
		opts.CacheDir = t.TempDir()
		res, err := Fetch(context.Background(), c, repo, opts)
		if err != nil {
			t.Fatal(err)
		}
		if c.calls != 3 || !reflect.DeepEqual(c.cursorsIn, []string{"", "c1", "c2"}) {
			t.Errorf("calls = %d cursors %v, want 3 calls with cursors '', c1, c2", c.calls, c.cursorsIn)
		}
		// #4 merged before the window (updated inside it); #1 is older than the window.
		if got := numbers(res.PRs); !reflect.DeepEqual(got, []int{5, 3, 2}) {
			t.Errorf("PRs in window = %v, want [5 3 2] (newest merge first)", got)
		}
		if res.Fetched != 4 || res.Capped || res.Partial {
			t.Errorf("result = %+v, want 4 fetched, not capped or partial", res)
		}
	})

	t.Run("rerun reads the cache and asks only for newer updates", func(t *testing.T) {
		dir := t.TempDir()
		opts := base
		opts.CacheDir = dir
		if _, err := Fetch(context.Background(), &fakeClient{pages: pages(), errAt: -1}, repo, opts); err != nil {
			t.Fatal(err)
		}
		opts.Now = day(30)
		c := &fakeClient{pages: []Page{{PRs: []PR{pr(6, 28, 28), pr(5, 20, 21)}, HasNextPage: true, EndCursor: "x", RateRemaining: 4000}}, errAt: -1}
		res, err := Fetch(context.Background(), c, repo, opts)
		if err != nil {
			t.Fatal(err)
		}
		if c.calls != 1 {
			t.Errorf("rerun made %d calls, want 1 (stops at PRs updated before the last fetch)", c.calls)
		}
		if got := numbers(res.PRs); !reflect.DeepEqual(got, []int{6, 5, 3, 2}) {
			t.Errorf("PRs = %v, want [6 5 3 2]", got)
		}
		if res.FromCache != 3 {
			t.Errorf("FromCache = %d, want 3", res.FromCache)
		}
	})

	t.Run("cap stops early and is reported", func(t *testing.T) {
		c := &fakeClient{pages: pages(), errAt: -1}
		opts := base
		opts.CacheDir, opts.Max = t.TempDir(), 3
		res, err := Fetch(context.Background(), c, repo, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Capped || res.Fetched != 3 || c.calls != 2 {
			t.Errorf("result = %+v after %d calls, want capped at 3 after 2 calls", res, c.calls)
		}
	})

	t.Run("low remaining quota stops with partial results", func(t *testing.T) {
		p := pages()
		p[0].RateRemaining = 10
		c := &fakeClient{pages: p, errAt: -1}
		opts := base
		opts.CacheDir = t.TempDir()
		res, err := Fetch(context.Background(), c, repo, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Partial || c.calls != 1 || res.PartialReason == "" {
			t.Errorf("result = %+v after %d calls, want partial after 1 call with a reason", res, c.calls)
		}
		if got := numbers(res.PRs); !reflect.DeepEqual(got, []int{5}) {
			t.Errorf("partial PRs = %v, want [5]", got)
		}
	})

	t.Run("rate-limit error keeps what was fetched", func(t *testing.T) {
		c := &fakeClient{pages: pages(), errAt: 1, err: fmt.Errorf("gh: %w", ErrRateLimited)}
		opts := base
		opts.CacheDir = t.TempDir()
		res, err := Fetch(context.Background(), c, repo, opts)
		if err != nil {
			t.Fatalf("rate limit should not be an error: %v", err)
		}
		if !res.Partial || res.Fetched != 2 {
			t.Errorf("result = %+v, want partial with 2 fetched", res)
		}
		// A partial run must not mark the window as covered.
		c2 := &fakeClient{pages: pages(), errAt: -1}
		if _, err := Fetch(context.Background(), c2, repo, opts); err != nil || c2.calls != 3 {
			t.Errorf("after a partial run, rerun made %d calls (err %v), want a full 3-call fetch", c2.calls, err)
		}
	})

	t.Run("other errors fail", func(t *testing.T) {
		c := &fakeClient{pages: pages(), errAt: 0, err: errors.New("boom")}
		opts := base
		opts.CacheDir = t.TempDir()
		if _, err := Fetch(context.Background(), c, repo, opts); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("cache files are keyed by repo and number", func(t *testing.T) {
		dir := t.TempDir()
		opts := base
		opts.CacheDir = dir
		if _, err := Fetch(context.Background(), &fakeClient{pages: pages(), errAt: -1}, repo, opts); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "o", "n", "pr-5.json")); err != nil {
			t.Errorf("missing cache file: %v", err)
		}
	})
}

func numbers(prs []PR) []int {
	out := []int{}
	for _, p := range prs {
		out = append(out, p.Number)
	}
	return out
}
