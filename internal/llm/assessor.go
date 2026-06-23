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
	"time"
)

const (
	DefaultModel    = "deepseek/deepseek-v4-flash"
	OpenRouterURL   = "https://openrouter.ai/api/v1/chat/completions"
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

// Assessor performs LLM-based blast radius analysis via OpenRouter
type Assessor struct {
	apiKey      string
	model       string
	cacheDir    string
	concurrency int
	client      *http.Client
}

// Config holds assessor configuration
type Config struct {
	APIKey      string
	Model       string
	CacheDir    string
	Concurrency int
}

// NewAssessor creates a new LLM assessor
func NewAssessor(cfg Config) (*Assessor, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY not set. Use --no-llm for static-only analysis")
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

	var assessCount, cacheCount int

	for _, f := range files {
		hash := contentHash(f.Content)

		if cached, ok := a.loadCache(hash); ok {
			mu.Lock()
			results[f.Path] = cached
			cacheCount++
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(file FileInput, h string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			assessment, err := a.assessFile(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: LLM assessment failed for %s: %v\n", file.Path, err)
				return
			}

			assessment.ContentHash = h
			a.saveCache(h, assessment)

			mu.Lock()
			results[file.Path] = assessment
			assessCount++
			mu.Unlock()
		}(f, hash)
	}

	wg.Wait()

	fmt.Printf("  LLM assessed %d files (%d from cache)\n", assessCount, cacheCount)

	return results, nil
}

// assessFile sends a single file to the LLM via OpenRouter
func (a *Assessor) assessFile(file FileInput) (*Assessment, error) {
	content := file.Content
	lines := strings.Split(content, "\n")
	if len(lines) > 500 {
		content = strings.Join(lines[:500], "\n") + "\n... (truncated)"
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
		req, err := http.NewRequest("POST", OpenRouterURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
		req.Header.Set("HTTP-Referer", "https://github.com/zanetworker/code-heatmap")
		req.Header.Set("X-Title", "Code Heatmap")

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
		"Respond with ONLY valid JSON on a single line. No markdown, no explanation, no newlines inside strings:\n\n" +
		"{\n" +
		"  \"security_impact\": <0-100>,\n" +
		"  \"data_impact\": <0-100>,\n" +
		"  \"availability_impact\": <0-100>,\n" +
		"  \"user_impact\": <0-100>,\n" +
		"  \"blast_radius_summary\": \"<one sentence: what breaks if this file has a bug>\",\n" +
		"  \"critical_reason\": \"<one phrase: why this matters, or empty string if low impact>\"\n" +
		"}\n\n" +
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
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		jsonStr := text[start : end+1]

		// Try as-is
		if err := json.Unmarshal([]byte(jsonStr), &assessment); err == nil {
			return clampAssessment(&assessment), nil
		}

		// Fix common issues: newlines inside string values
		cleaned := cleanJSON(jsonStr)
		if err := json.Unmarshal([]byte(cleaned), &assessment); err == nil {
			return clampAssessment(&assessment), nil
		}
	}

	return nil, fmt.Errorf("could not parse assessment from: %s", text[:min(len(text), 200)])
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
