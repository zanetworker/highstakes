package llm

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	DefaultModel  = "openai/gpt-6-luna"
	DefaultAPIURL = "https://openrouter.ai/api/v1/chat/completions"

	// PromptVersion is part of the cache key; bump it when buildPrompt changes.
	PromptVersion = "3"
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
	retryDelay  time.Duration // base backoff unit between retries
	variant     int32         // index into requestVariants that last worked (atomic)
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
		retryDelay:  time.Second,
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
		// Credentials never reach a prompt, even if the caller passes them.
		if SensitivePath(f.Path) || SensitiveContent(f.Content) {
			continue
		}
		if c, ok := a.loadCache(a.cacheKey(f.Content)); ok {
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
		key := a.cacheKey(f.Content)
		wg.Add(1)
		go func(file FileInput, key string) {
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

			assessment.ContentHash = contentHash(file.Content)
			a.saveCache(key, assessment)

			mu.Lock()
			results[file.Path] = assessment
			mu.Unlock()

			// Progress every 10 files or on last file
			if n%10 == 0 || int(n) == needAssess {
				fmt.Fprintf(os.Stderr, "  [%d/%d] assessed\n", n, needAssess)
			}
		}(f, key)
	}

	wg.Wait()

	assessed := int(done) - int(failed)
	fmt.Fprintf(os.Stderr, "  Done: %d assessed, %d cached, %d failed\n", assessed, cached, failed)

	return results, nil
}

// assessmentSchema is the structured-output contract for one assessment.
var assessmentSchema = func() map[string]interface{} {
	score := map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 100}
	text := map[string]interface{}{"type": "string"}
	return map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "blast_radius_assessment",
			"strict": true,
			"schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"security_impact":      score,
					"data_impact":          score,
					"availability_impact":  score,
					"user_impact":          score,
					"blast_radius_summary": text,
					"critical_reason":      text,
				},
				"required":             []string{"security_impact", "data_impact", "availability_impact", "user_impact", "blast_radius_summary", "critical_reason"},
				"additionalProperties": false,
			},
		},
	}
}()

// assessFile sends a single file to the LLM via OpenRouter
func (a *Assessor) assessFile(file FileInput) (*Assessment, error) {
	content := file.Content
	lines := strings.Split(content, "\n")
	if len(lines) > 300 {
		content = strings.Join(lines[:300], "\n") + "\n... (truncated)"
	}

	prompt := buildPrompt(file.Path, content)

	// Try the strictest request the model's providers accept, starting from
	// the variant that last worked; a variant is remembered once it succeeds.
	start := int(atomic.LoadInt32(&a.variant))
	var lastErr error
	for v := start; v < len(requestVariants); v++ {
		assessment, err := a.request(requestVariants[v](a.model, prompt))
		if errors.Is(err, errUnsupportedParams) {
			lastErr = err
			continue
		}
		if err == nil && v != start {
			atomic.StoreInt32(&a.variant, int32(v))
		}
		return assessment, err
	}
	return nil, lastErr
}

// errUnsupportedParams marks OpenRouter's 404 when no provider for the model
// supports every requested parameter.
var errUnsupportedParams = errors.New("no provider supports the requested parameters")

// requestVariants go from strictest to most permissive. All ask for the JSON
// schema and route only to providers that honour every parameter. Reasoning is
// off where possible (it can use the whole token budget, leaving no answer);
// temperature 0 keeps answers repeatable. Providers differ per model: DeepSeek
// takes all three, GLM rejects temperature, Haiku and GPT Luna reject both.
var requestVariants = []func(model, prompt string) map[string]interface{}{
	func(model, prompt string) map[string]interface{} {
		b := baseRequest(model, prompt)
		b["temperature"] = 0
		b["reasoning"] = map[string]interface{}{"effort": "none"}
		return b
	},
	func(model, prompt string) map[string]interface{} {
		b := baseRequest(model, prompt)
		b["reasoning"] = map[string]interface{}{"effort": "none"}
		return b
	},
	baseRequest,
}

// baseRequest is an OpenAI-compatible request (works for all OpenRouter
// models). max_tokens leaves room for models that reason anyway.
func baseRequest(model, prompt string) map[string]interface{} {
	return map[string]interface{}{
		"model":           model,
		"max_tokens":      4096,
		"response_format": assessmentSchema,
		"provider":        map[string]interface{}{"require_parameters": true},
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
}

// request sends one request body with retries and parses the assessment.
func (a *Assessor) request(reqBody map[string]interface{}) (*Assessment, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Retry with backoff
	const maxAttempts = 3
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequest("POST", a.apiURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
		req.Header.Set("HTTP-Referer", "https://github.com/zanetworker/highstakes")
		req.Header.Set("X-Title", "HighStakes")

		r, err := a.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("API request failed after %d attempts: %w", maxAttempts, err)
			time.Sleep(time.Duration(attempt+1) * a.retryDelay)
			continue
		}
		if r.StatusCode == http.StatusTooManyRequests {
			r.Body.Close()
			lastErr = fmt.Errorf("API rate limited (429) after %d attempts", maxAttempts)
			time.Sleep(time.Duration(attempt+1) * 2 * a.retryDelay)
			continue
		}
		resp = r
		break
	}

	if resp == nil {
		return nil, lastErr
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		if (resp.StatusCode == http.StatusNotFound && strings.Contains(string(respBody), "requested parameters")) ||
			(resp.StatusCode == http.StatusBadRequest && strings.Contains(string(respBody), "cannot be disabled")) {
			return nil, fmt.Errorf("%w: %s", errUnsupportedParams, string(respBody))
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// OpenAI-compatible response format
	var apiResp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
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
	choice := apiResp.Choices[0]
	if choice.Message.Content == "" && choice.FinishReason == "length" {
		return nil, fmt.Errorf("no answer: the model hit the token limit (likely spent on reasoning)")
	}

	return parseAssessment(choice.Message.Content)
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

		candidate += "}"

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
	a.SecurityImpact = clamp0to100(a.SecurityImpact)
	a.DataImpact = clamp0to100(a.DataImpact)
	a.AvailabilityImpact = clamp0to100(a.AvailabilityImpact)
	a.UserImpact = clamp0to100(a.UserImpact)
	return a
}

func clamp0to100(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Cache operations

func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:16])
}

// cacheKey identifies an assessment by content, model and prompt version,
// so switching models or prompts never reuses a stale assessment.
func (a *Assessor) cacheKey(content string) string {
	return contentHash(a.model + "\x00" + PromptVersion + "\x00" + content)
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

	_ = os.WriteFile(a.cachePath(hash), data, 0644)
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

	if strings.HasPrefix(base, "zz_generated") {
		return false
	}

	// Lock files are generated; config files (YAML, TOML, JSON, Dockerfile, Terraform) are assessed.
	lockFiles := map[string]bool{
		"package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
		"npm-shrinkwrap.json": true, "composer.lock": true,
	}
	if lockFiles[base] {
		return false
	}

	skipExts := map[string]bool{
		".md": true, ".lock": true, ".sum": true, ".mod": true,
	}

	return !skipExts[ext]
}
