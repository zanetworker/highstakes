package ownership

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestParseLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Commit
	}{
		{
			name: "two commits with numstat",
			input: "\x1eAlice\x1falice@x.com\x1f1700000000\n\n10\t2\ta.go\n3\t0\tb/c.go\n" +
				"\x1eBob\x1fbob@x.com\x1f1700000100\n\n1\t1\ta.go\n",
			want: []Commit{
				{Name: "Alice", Email: "alice@x.com", When: time.Unix(1700000000, 0), Files: map[string]int{"a.go": 12, "b/c.go": 3}},
				{Name: "Bob", Email: "bob@x.com", When: time.Unix(1700000100, 0), Files: map[string]int{"a.go": 2}},
			},
		},
		{
			name:  "binary file counts as one line",
			input: "\x1eAlice\x1falice@x.com\x1f1700000000\n\n-\t-\timg.png\n",
			want:  []Commit{{Name: "Alice", Email: "alice@x.com", When: time.Unix(1700000000, 0), Files: map[string]int{"img.png": 1}}},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "commit with no files is kept with empty map",
			input: "\x1eAlice\x1falice@x.com\x1f1700000000\n",
			want:  []Commit{{Name: "Alice", Email: "alice@x.com", When: time.Unix(1700000000, 0), Files: map[string]int{}}},
		},
		{
			name:  "malformed header is skipped with its files",
			input: "\x1egarbage\n1\t1\ta.go\n\x1eBob\x1fbob@x.com\x1f1700000100\n\n1\t0\ta.go\n",
			want:  []Commit{{Name: "Bob", Email: "bob@x.com", When: time.Unix(1700000100, 0), Files: map[string]int{"a.go": 1}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLog(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d commits, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				g, w := got[i], tt.want[i]
				if g.Name != w.Name || g.Email != w.Email || !g.When.Equal(w.When) {
					t.Errorf("commit %d header = %s/%s/%v, want %s/%s/%v", i, g.Name, g.Email, g.When, w.Name, w.Email, w.When)
				}
				if len(g.Files) != len(w.Files) {
					t.Errorf("commit %d files = %v, want %v", i, g.Files, w.Files)
				}
				for f, n := range w.Files {
					if g.Files[f] != n {
						t.Errorf("commit %d file %s lines = %d, want %d", i, f, g.Files[f], n)
					}
				}
			}
		})
	}
}

func TestHandleFor(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		aliases map[string]string
		want    string
	}{
		{"alias wins", "alice@x.com", map[string]string{"alice@x.com": "@alice-gh"}, "@alice-gh"},
		{"alias lookup is case-insensitive", "Alice@X.com", map[string]string{"Alice@x.com": "@alice-gh"}, "@alice-gh"},
		{"alias without @ gets one", "alice@x.com", map[string]string{"alice@x.com": "alice-gh"}, "@alice-gh"},
		{"github noreply with id", "12345+octo@users.noreply.github.com", nil, "@octo"},
		{"github noreply without id", "octo@users.noreply.github.com", nil, "@octo"},
		{"plain email stays email", "bob@x.com", nil, "bob@x.com"},
		{"empty email", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HandleFor(tt.email, tt.aliases); got != tt.want {
				t.Errorf("HandleFor(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

func TestCompute(t *testing.T) {
	day := 24 * time.Hour
	commits := []Commit{
		{Name: "Alice", Email: "alice@x.com", When: now.Add(-10 * day), Files: map[string]int{"a.go": 100}},
		{Name: "Bob", Email: "bob@x.com", When: now.Add(-1000 * day), Files: map[string]int{"a.go": 100, "b.go": 5}},
		{Name: "Carol", Email: "carol@x.com", When: now.Add(-1 * day), Files: map[string]int{"a.go": 10}},
		{Name: "dependabot[bot]", Email: "49699333+dependabot[bot]@users.noreply.github.com", When: now, Files: map[string]int{"a.go": 500}},
		{Name: "Alice", Email: "ALICE@x.com", When: now.Add(-5 * day), Files: map[string]int{"b.go": 1}},
	}
	opts := Options{Now: now, HalfLifeDays: 180, Aliases: map[string]string{"carol@x.com": "@carol"}}
	got := Compute(commits, opts)

	t.Run("recent large author ranks first, old author decays", func(t *testing.T) {
		owners := got["a.go"]
		if len(owners) != 3 {
			t.Fatalf("a.go owners = %+v, want 3 (bot excluded)", owners)
		}
		if owners[0].Email != "alice@x.com" {
			t.Errorf("top owner = %s, want alice@x.com", owners[0].Email)
		}
		// Bob's 100 lines are 1000 days old: weight 2^(-1000/180) ~ 0.021, about 2 points vs Carol's ~10.
		if owners[1].Email != "carol@x.com" || owners[1].Handle != "@carol" {
			t.Errorf("second owner = %+v, want carol with alias @carol", owners[1])
		}
	})

	t.Run("shares sum to one", func(t *testing.T) {
		sum := 0.0
		for _, o := range got["a.go"] {
			sum += o.Share
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("shares sum = %f, want 1", sum)
		}
	})

	t.Run("emails are merged case-insensitively and commits counted", func(t *testing.T) {
		owners := got["b.go"]
		if len(owners) != 2 {
			t.Fatalf("b.go owners = %+v, want 2", owners)
		}
		for _, o := range owners {
			if o.Email == "alice@x.com" && o.Commits != 1 {
				t.Errorf("alice commits on b.go = %d, want 1", o.Commits)
			}
		}
	})

	t.Run("bot-only file has no owners", func(t *testing.T) {
		got := Compute([]Commit{{Name: "renovate[bot]", Email: "bot@x.com", When: now, Files: map[string]int{"c.go": 3}}}, opts)
		if len(got["c.go"]) != 0 {
			t.Errorf("c.go owners = %+v, want none", got["c.go"])
		}
	})

	t.Run("no commits gives empty map", func(t *testing.T) {
		if got := Compute(nil, opts); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

// commitAs writes content to file and commits it with the given author.
func commitAs(t *testing.T, repo *git.Repository, dir, file, content, name, email string, when time.Time) {
	t.Helper()
	full := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(file); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: name, Email: email, When: when}
	if _, err := wt.Commit("change "+file, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}
}

func TestFileOwners_TempRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	recent := time.Now().Add(-24 * time.Hour)
	commitAs(t, repo, dir, "core/auth.go", "a\nb\nc\nd\ne\n", "Alice", "alice@old.com", recent.Add(-time.Hour))
	commitAs(t, repo, dir, "core/auth.go", "a\nb\nc\nd\ne\nf\n", "Bob", "bob@x.com", recent)
	commitAs(t, repo, dir, "core/auth.go", "a\nb\nc\nd\ne\nf\ng\nh\ni\n", "Alice", "alice@new.com", recent)
	commitAs(t, repo, dir, "docs.txt", "x\n", "Bob", "bob@x.com", recent)
	commitAs(t, repo, dir, "core/café.go", "x\n", "Carol", "carol@x.com", recent)
	// .mailmap folds alice@old.com into alice@new.com
	commitAs(t, repo, dir, ".mailmap", "Alice <alice@new.com> <alice@old.com>\n", "Bob", "bob@x.com", recent)

	t.Run("mailmap merges identities and alias maps handle", func(t *testing.T) {
		got, err := FileOwners(dir, []string{"core/auth.go"}, Options{Aliases: map[string]string{"alice@new.com": "@alice"}})
		if err != nil {
			t.Fatal(err)
		}
		owners := got["core/auth.go"]
		if len(owners) != 2 {
			t.Fatalf("owners = %+v, want 2 (alice merged via mailmap, bob)", owners)
		}
		if owners[0].Handle != "@alice" || owners[0].Commits != 2 {
			t.Errorf("top owner = %+v, want @alice with 2 commits", owners[0])
		}
		if owners[1].Email != "bob@x.com" {
			t.Errorf("second owner = %+v, want bob@x.com", owners[1])
		}
	})

	t.Run("path filter limits results", func(t *testing.T) {
		got, err := FileOwners(dir, []string{"core/auth.go"}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["docs.txt"]; ok {
			t.Errorf("docs.txt should not be in results: %v", got)
		}
	})

	t.Run("nil paths returns all files", func(t *testing.T) {
		got, err := FileOwners(dir, nil, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got["docs.txt"]) != 1 {
			t.Errorf("docs.txt owners = %v, want 1", got["docs.txt"])
		}
	})

	t.Run("non-ASCII path keeps its owner", func(t *testing.T) {
		got, err := FileOwners(dir, []string{"core/café.go"}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		owners := got["core/café.go"]
		if len(owners) != 1 || owners[0].Email != "carol@x.com" {
			t.Errorf("core/café.go owners = %+v, want carol@x.com (got keys %v)", owners, got)
		}
	})

	t.Run("not a repo returns error", func(t *testing.T) {
		if _, err := FileOwners(t.TempDir(), nil, Options{}); err == nil {
			t.Error("want error for non-repo dir")
		}
	})
}
