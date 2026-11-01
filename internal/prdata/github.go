package prdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Repo is a GitHub repository.
type Repo struct {
	Owner, Name string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ParseRepo parses "owner/name".
func ParseRepo(s string) (Repo, error) {
	owner, name, ok := strings.Cut(s, "/")
	if !ok || !repoPart.MatchString(owner) || !repoPart.MatchString(name) {
		return Repo{}, fmt.Errorf("want owner/name, got %q", s)
	}
	return Repo{owner, name}, nil
}

var githubRemote = regexp.MustCompile(`github\.com[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(\.git)?/?$`)

// RepoFromRemoteURL extracts owner/name from a GitHub remote URL (https,
// ssh or scp-style).
func RepoFromRemoteURL(url string) (Repo, error) {
	m := githubRemote.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return Repo{}, fmt.Errorf("not a GitHub remote URL: %q", url)
	}
	return Repo{m[1], m[2]}, nil
}

// OriginRepo reads the GitHub repository of the origin remote in repoDir.
func OriginRepo(repoDir string) (Repo, error) {
	out, err := exec.Command("git", "-C", repoDir, "remote", "get-url", "origin").Output()
	if err != nil {
		return Repo{}, fmt.Errorf("read origin remote in %s: %w", repoDir, err)
	}
	return RepoFromRemoteURL(string(out))
}

// Page is one page of merged PRs, newest update first.
type Page struct {
	PRs           []PR
	HasNextPage   bool
	EndCursor     string
	RateRemaining int // GraphQL points left; negative when unknown
}

// Client lists merged PRs. Implementations return them ordered by update
// time, newest first, and wrap ErrRateLimited when GitHub refuses for quota.
type Client interface {
	MergedPRsPage(ctx context.Context, repo Repo, cursor string) (Page, error)
}

// ErrRateLimited means GitHub refused a request for rate limiting.
var ErrRateLimited = errors.New("GitHub rate limit reached")

// prQuery fetches 50 merged PRs per page with their labels, closing issues'
// labels and reviews. Review and label lists are capped; totalCount still
// gives the full review count.
const prQuery = `query($owner:String!,$name:String!,$cursor:String){
  rateLimit{remaining}
  repository(owner:$owner,name:$name){
    pullRequests(states:MERGED,first:50,after:$cursor,orderBy:{field:UPDATED_AT,direction:DESC}){
      pageInfo{hasNextPage endCursor}
      nodes{
        number title mergedAt updatedAt
        mergeCommit{oid}
        author{login}
        labels(first:20){nodes{name}}
        closingIssuesReferences(first:5){nodes{number labels(first:10){nodes{name}}}}
        reviews(first:30){totalCount nodes{state author{login}}}
      }
    }
  }
}`

// GHClient queries GitHub GraphQL through the gh CLI, which supplies auth.
type GHClient struct{}

// MergedPRsPage implements Client.
func (GHClient) MergedPRsPage(ctx context.Context, repo Repo, cursor string) (Page, error) {
	args := []string{"api", "graphql", "-f", "query=" + prQuery, "-F", "owner=" + repo.Owner, "-F", "name=" + repo.Name}
	if cursor != "" {
		args = append(args, "-f", "cursor="+cursor)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(msg), "rate limit") {
			return Page{}, fmt.Errorf("gh api graphql for %s: %s: %w", repo, msg, ErrRateLimited)
		}
		// gh exits non-zero on GraphQL errors but still prints the body.
		if len(out) > 0 {
			if _, derr := decodePage(out); derr != nil {
				return Page{}, fmt.Errorf("gh api graphql for %s: %w", repo, derr)
			}
		}
		return Page{}, fmt.Errorf("gh api graphql for %s: %v: %s", repo, err, msg)
	}
	return decodePage(out)
}

type gqlResponse struct {
	Data struct {
		RateLimit *struct {
			Remaining int `json:"remaining"`
		} `json:"rateLimit"`
		Repository *struct {
			PullRequests struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []gqlPR `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"errors"`
}

type gqlNames struct {
	Nodes []struct {
		Name string `json:"name"`
	} `json:"nodes"`
}

type gqlPR struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	MergedAt    time.Time `json:"mergedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	MergeCommit *struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels        gqlNames `json:"labels"`
	ClosingIssues struct {
		Nodes []struct {
			Number int      `json:"number"`
			Labels gqlNames `json:"labels"`
		} `json:"nodes"`
	} `json:"closingIssuesReferences"`
	Reviews struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			State  string `json:"state"`
			Author *struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"nodes"`
	} `json:"reviews"`
}

// decodePage parses a GraphQL response body for prQuery.
func decodePage(body []byte) (Page, error) {
	var r gqlResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return Page{}, fmt.Errorf("decode GraphQL response: %w", err)
	}
	if len(r.Errors) > 0 {
		e := r.Errors[0]
		if e.Type == "RATE_LIMITED" || strings.Contains(strings.ToLower(e.Message), "rate limit") {
			return Page{}, fmt.Errorf("%s: %w", e.Message, ErrRateLimited)
		}
		return Page{}, fmt.Errorf("GraphQL error %s: %s", e.Type, e.Message)
	}
	if r.Data.Repository == nil {
		return Page{}, errors.New("GraphQL response has no repository")
	}
	prs := r.Data.Repository.PullRequests
	page := Page{HasNextPage: prs.PageInfo.HasNextPage, EndCursor: prs.PageInfo.EndCursor, RateRemaining: -1}
	if r.Data.RateLimit != nil {
		page.RateRemaining = r.Data.RateLimit.Remaining
	}
	for _, n := range prs.Nodes {
		p := PR{Number: n.Number, Title: n.Title, MergedAt: n.MergedAt, UpdatedAt: n.UpdatedAt, Reviews: n.Reviews.TotalCount}
		if n.MergeCommit != nil {
			p.MergeCommit = n.MergeCommit.OID
		}
		if n.Author != nil {
			p.Author = n.Author.Login
		}
		for _, l := range n.Labels.Nodes {
			p.Labels = append(p.Labels, l.Name)
		}
		for _, is := range n.ClosingIssues.Nodes {
			p.ClosingIssues = append(p.ClosingIssues, is.Number)
			for _, l := range is.Labels.Nodes {
				p.ClosingIssueLabels = append(p.ClosingIssueLabels, l.Name)
			}
		}
		approvers := map[string]bool{}
		for _, rv := range n.Reviews.Nodes {
			if rv.State == "APPROVED" && rv.Author != nil {
				approvers[rv.Author.Login] = true
			}
		}
		p.Approvers = len(approvers)
		page.PRs = append(page.PRs, p)
	}
	return page, nil
}

// FetchOptions controls Fetch.
type FetchOptions struct {
	Since, Now   time.Time // keep PRs merged in (Since, Now]
	Max          int       // stop after reading this many PRs from the API
	MinRemaining int       // stop when fewer GraphQL points than this remain
	CacheDir     string    // per-repo cache lives in CacheDir/<owner>/<name>/
}

// FetchResult is the outcome of Fetch.
type FetchResult struct {
	PRs           []PR   `json:"-"`          // merged in the window, newest merge first
	Fetched       int    `json:"fetched"`    // PRs read from the API this run
	FromCache     int    `json:"from_cache"` // PRs in the window served from the cache
	APICalls      int    `json:"api_calls"`
	Capped        bool   `json:"capped"`
	Cap           int    `json:"cap"`
	Partial       bool   `json:"partial"`
	PartialReason string `json:"partial_reason,omitempty"`
}

type cacheIndex struct {
	CoveredFrom time.Time `json:"covered_from"` // every PR merged after this was fetched...
	FetchedAt   time.Time `json:"fetched_at"`   // ...as of this time
}

// Fetch returns the PRs merged in the window, reading the disk cache and
// asking the API only for what it lacks: after a complete run it fetches only
// PRs updated since then. It stops early, keeping what it has, at the Max cap,
// when the remaining rate-limit quota falls under MinRemaining, or when
// GitHub reports a rate limit; those runs are not recorded as complete.
func Fetch(ctx context.Context, c Client, repo Repo, opts FetchOptions) (FetchResult, error) {
	res := FetchResult{Cap: opts.Max}
	dir := filepath.Join(opts.CacheDir, repo.Owner, repo.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("create PR cache %s: %w", dir, err)
	}
	stopAt := opts.Since
	var idx cacheIndex
	if b, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil && json.Unmarshal(b, &idx) == nil &&
		!idx.CoveredFrom.After(opts.Since) {
		stopAt = idx.FetchedAt
	}

	fetchedNow := map[int]bool{}
	cursor := ""
loop:
	for {
		page, err := c.MergedPRsPage(ctx, repo, cursor)
		res.APICalls++
		if errors.Is(err, ErrRateLimited) {
			res.Partial, res.PartialReason = true, err.Error()
			break
		}
		if err != nil {
			return res, err
		}
		for _, p := range page.PRs {
			if p.UpdatedAt.Before(stopAt) {
				break loop
			}
			if err := writeJSON(filepath.Join(dir, fmt.Sprintf("pr-%d.json", p.Number)), p); err != nil {
				return res, err
			}
			fetchedNow[p.Number] = true
			res.Fetched++
			if opts.Max > 0 && res.Fetched >= opts.Max {
				res.Capped = true
				break loop
			}
		}
		if !page.HasNextPage {
			break
		}
		if page.RateRemaining >= 0 && page.RateRemaining < opts.MinRemaining {
			res.Partial = true
			res.PartialReason = fmt.Sprintf("stopped with %d GraphQL points left (under %d)", page.RateRemaining, opts.MinRemaining)
			break
		}
		cursor = page.EndCursor
	}
	if !res.Capped && !res.Partial {
		covered := opts.Since
		if stopAt.Equal(idx.FetchedAt) && !idx.CoveredFrom.IsZero() && idx.CoveredFrom.Before(covered) {
			covered = idx.CoveredFrom
		}
		if err := writeJSON(filepath.Join(dir, "index.json"), cacheIndex{CoveredFrom: covered, FetchedAt: opts.Now}); err != nil {
			return res, err
		}
	}

	all, err := readCachedPRs(dir)
	if err != nil {
		return res, err
	}
	for _, p := range all {
		if p.MergedAt.After(opts.Since) && !p.MergedAt.After(opts.Now) {
			res.PRs = append(res.PRs, p)
			if !fetchedNow[p.Number] {
				res.FromCache++
			}
		}
	}
	sort.Slice(res.PRs, func(i, j int) bool {
		if !res.PRs[i].MergedAt.Equal(res.PRs[j].MergedAt) {
			return res.PRs[i].MergedAt.After(res.PRs[j].MergedAt)
		}
		return res.PRs[i].Number > res.PRs[j].Number
	})
	return res, nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readCachedPRs(dir string) ([]PR, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "pr-*.json"))
	if err != nil {
		return nil, err
	}
	var out []PR
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", m, err)
		}
		var p PR
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("decode cached PR %s: %w", m, err)
		}
		out = append(out, p)
	}
	return out, nil
}
