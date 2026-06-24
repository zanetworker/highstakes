package llm

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultModel  = "deepseek/deepseek-v4-flash"
	DefaultAPIURL = "https://openrouter.ai/api/v1/chat/completions"
)

// Assessment holds the LLM's blast radius evaluation for a single file
type Assessment struct {
	SecurityImpact     int    `json:"security_impact"`     // 0-100
	DataImpact         int    `json:"data_impact"`         // 0-100
	AvailabilityImpact int    `json:"availability_impact"` // 0-100
	UserImpact         int    `json:"user_impact"`         // 0-100
	BlastRadiusSummary string `json:"blast_radius_summary"`
	CriticalReason     string `json:"critical_reason"`
	ContentHash        string `json:"content_hash"`
}

// MaxScore returns the highest impact dimension
func (a Assessment) MaxScore() int {
	m := a.SecurityImpact
	if a.DataImpact > m {
		m = a.DataImpact
	}
	if a.AvailabilityImpact > m {
		m = a.AvailabilityImpact
	}
	if a.UserImpact > m {
		m = a.UserImpact
	}
	return m
}

// Assessor performs LLM-based blast radius analysis
type Assessor struct {
	apiKey      string
	apiURL      string
	model       string
	cacheDir    string
	concurrency int
	client      *http.Client
}

// Config holds assessor configuration
type Config struct {
	APIKey      string
	APIURL      string
	Model       string
	CacheDir    string
	Concurrency int
}

// NewAssessor creates a new LLM assessor
func NewAssessor(cfg Config) (*Assessor, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key set. Set OPENROUTER_API_KEY, or HIGHSTAKES_API_KEY + HIGHSTAKES_API_URL for any OpenAI-compatible endpoint. Use --no-llm for static-only analysis")
	}

	if cfg.APIURL == "" {
		cfg.APIURL = DefaultAPIURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = ".heatmap/cache"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}

	if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	return &Assessor{
		apiKey:      cfg.APIKey,
		apiURL:      cfg.APIURL,
		model:       cfg.Model,
		cacheDir:    cfg.CacheDir,
		concurrency: cfg.Concurrency,
		client:      &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// FileInput represents a file to assess
type FileInput struct {
	Path    string
	Content string
}

// AssessFiles evaluates multiple files concurrently, using cache when available
func (a *Assessor) AssessFiles(files []FileInput) (map[string]*Assessment, error) {
	results := make(map[string]*Assessment)
	var mu sync.Mutex
	var wg sync.WaitGroup

	sem := make(chan struct{}, a.concurrency)

	total := len(files)
	var done, cached, failed int32

	// Count cached files first
	var toAssess []FileInput
	for _, f := range files {
		hash := contentHash(f.Content)
		if c, ok := a.loadCache(hash); ok {
			mu.Lock()
			results[f.Path] = c
			mu.Unlock()
			atomic.AddInt32(&cached, 1)
		} else {
			toAssess = append(toAssess, f)
		}
	}

	needAssess := len(toAssess)
	if cached > 0 {
		fmt.Fprintf(os.Stderr, "  %d/%d from cache, assessing %d files...\n", cached, total, needAssess)
	}

	for _, f := range toAssess {
		hash := contentHash(f.Content)
		wg.Add(1)
		go func(file FileInput, h string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			assessment, err := a.assessFile(file)
			n := atomic.AddInt32(&done, 1)

			if err != nil {
				atomic.AddInt32(&failed, 1)
				fmt.Fprintf(os.Stderr, "  [%d/%d] FAIL %s: %v\n", n, needAssess, file.Path, err)
				return
			}

			assessment.ContentHash = h
			a.saveCache(h, assessment)

			mu.Lock()
			results[file.Path] = assessment
			mu.Unlock()

			// Progress every 10 files or on last file
			if n%10 == 0 || int(n) == needAssess {
				fmt.Fprintf(os.Stderr, "  [%d/%d] assessed\n", n, needAssess)
			}
		}(f, hash)
	}

	wg.Wait()

	assessed := int(done) - int(failed)
	fmt.Fprintf(os.Stderr, "  Done: %d assessed, %d cached, %d failed\n", assessed, cached, failed)

	return results, nil
}

// assessFile sends a single file to the LLM via OpenRouter
func (a *Assessor) assessFile(file FileInput) (*Assessment, error) {
	content := file.Content
	lines := strings.Split(content, "\n")
	if len(lines) > 300 {
		content = strings.Join(lines[:300], "\n") + "\n... (truncated)"
	}

	prompt := buildPrompt(file.Path, content)

	// OpenAI-compatible request (works for all OpenRouter models)
	reqBody := map[string]interface{}{
		"model":      a.model,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Retry with backoff
	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest("POST", a.apiURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
		req.Header.Set("HTTP-Referer", "https://github.com/zanetworker/highstakes")
		req.Header.Set("X-Title", "HighStakes")

		resp, err = a.client.Do(req)
		if err != nil {
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		break
	}

	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// OpenAI-compatible response format
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return parseAssessment(apiResp.Choices[0].Message.Content)
}

func buildPrompt(path, content string) string {
	return "Analyze this source file and assess its blast radius: if this code has a bug or breaks, what is the impact?\n\n" +
		"File: " + path + "\n\n" +
		content + "\n\n" +
		"Respond with ONLY a single-line JSON object. No markdown. Keep strings under 100 characters.\n\n" +
		"{\"security_impact\":<0-100>,\"data_impact\":<0-100>,\"availability_impact\":<0-100>,\"user_impact\":<0-100>,\"blast_radius_summary\":\"<max 80 chars>\",\"critical_reason\":\"<max 40 chars or empty>\"}\n\n" +
		"Scoring guide:\n" +
		"- 0-20: Internal helper, formatting, logging. Breakage causes cosmetic issues only.\n" +
		"- 21-40: Utility code. Breakage causes minor functionality loss.\n" +
		"- 41-60: Business logic. Breakage causes features to malfunction.\n" +
		"- 61-80: Core infrastructure. Breakage causes service degradation or partial outage.\n" +
		"- 81-100: Security boundary, auth, crypto, sandbox isolation, data protection. Breakage causes security breach, data loss, or complete outage.\n\n" +
		"A simple 10-line auth check is more critical than a 500-line log formatter. Score based on WHAT the code does, not how complex it is."
}

// parseAssessment extracts the Assessment JSON from LLM response text
func parseAssessment(text string) (*Assessment, error) {
	var assessment Assessment

	// Try direct parse
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &assessment); err == nil {
		return clampAssessment(&assessment), nil
	}

	// Extract JSON block
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON found in response")
	}

	jsonStr := text[start:]

	// Clean newlines inside strings
	jsonStr = cleanJSON(jsonStr)

	// Try as-is
	if err := json.Unmarshal([]byte(jsonStr), &assessment); err == nil {
		return clampAssessment(&assessment), nil
	}

	// Try truncating to last complete }
	end := strings.LastIndex(jsonStr, "}")
	if end > 0 {
		if err := json.Unmarshal([]byte(jsonStr[:end+1]), &assessment); err == nil {
			return clampAssessment(&assessment), nil
		}
	}

	// Repair truncated JSON: close any open strings and braces
	repaired := repairJSON(jsonStr)
	if err := json.Unmarshal([]byte(repaired), &assessment); err == nil {
		return clampAssessment(&assessment), nil
	}

	return nil, fmt.Errorf("could not parse assessment from: %s", text[:min(len(text), 200)])
}

// repairJSON attempts to close truncated JSON by adding missing quotes and braces
func repairJSON(s string) string {
	s = strings.TrimSpace(s)

	// Find last complete key-value pair by looking for last successful comma or opening brace
	// Then truncate everything after it and close
	lastComma := strings.LastIndex(s, ",")
	lastBrace := strings.LastIndex(s, "{")

	cutPoint := lastComma
	if lastBrace > lastComma {
		cutPoint = lastBrace
	}

	if cutPoint > 0 {
		// Try truncating to last comma (drop incomplete field)
		candidate := s[:cutPoint]

		// If we cut at a comma, just close the brace
		if s[cutPoint] == ',' {
			candidate += "}"
		} else {
			candidate += "}"
		}

		var test Assessment
		if err := json.Unmarshal([]byte(candidate), &test); err == nil {
			return candidate
		}
	}

	// Fallback: close any open strings and braces
	inString := false
	escaped := false
	for _, ch := range s {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
		}
	}
	if inString {
		s += `"`
	}

	opens := strings.Count(s, "{") - strings.Count(s, "}")
	for i := 0; i < opens; i++ {
		s += "}"
	}

	return s
}

func cleanJSON(s string) string {
	// Replace literal newlines inside JSON string values with spaces
	inString := false
	escaped := false
	var out strings.Builder
	for _, ch := range s {
		if escaped {
			out.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			out.WriteRune(ch)
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
		}
		if inString && ch == '\n' {
			out.WriteRune(' ')
			continue
		}
		out.WriteRune(ch)
	}
	return out.String()
}

func clampAssessment(a *Assessment) *Assessment {
	a.SecurityImpact = clamp(a.SecurityImpact, 0, 100)
	a.DataImpact = clamp(a.DataImpact, 0, 100)
	a.AvailabilityImpact = clamp(a.AvailabilityImpact, 0, 100)
	a.UserImpact = clamp(a.UserImpact, 0, 100)
	return a
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Cache operations

func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:16])
}

func (a *Assessor) cachePath(hash string) string {
	return filepath.Join(a.cacheDir, hash+".json")
}

func (a *Assessor) loadCache(hash string) (*Assessment, bool) {
	data, err := os.ReadFile(a.cachePath(hash))
	if err != nil {
		return nil, false
	}

	var assessment Assessment
	if err := json.Unmarshal(data, &assessment); err != nil {
		return nil, false
	}

	return &assessment, true
}

func (a *Assessor) saveCache(hash string, assessment *Assessment) {
	data, err := json.MarshalIndent(assessment, "", "  ")
	if err != nil {
		return
	}

	os.WriteFile(a.cachePath(hash), data, 0644)
}

// ShouldAssess returns true if the file should be sent to the LLM
func ShouldAssess(path string) bool {
	base := filepath.Base(path)
	ext := filepath.Ext(path)

	testSuffixes := []string{"_test.go", "_test.rs", ".test.ts", ".test.js", ".spec.ts", ".spec.js", "_test.py"}
	for _, s := range testSuffixes {
		if strings.HasSuffix(base, s) {
			return false
		}
	}
	if strings.HasPrefix(base, "test_") {
		return false
	}

	if strings.HasSuffix(base, ".pb.go") || strings.Contains(base, ".generated.") {
		return false
	}

	skipExts := map[string]bool{
		".md": true, ".yaml": true, ".yml": true, ".toml": true,
		".json": true, ".lock": true, ".sum": true, ".mod": true,
	}
	if skipExts[ext] {
		return false
	}

	return true
}
