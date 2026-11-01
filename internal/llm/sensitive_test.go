package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSensitivePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"credentials.json", true},
		{"config/auth.json", true},
		{".env", true},
		{".env.local", true},
		{"deploy/tls.pem", true},
		{"certs/server.key", true},
		{"keys/id_rsa", true},
		{"keys/id_rsa.pub", true},
		{"keys/id_ed25519", true},
		{"gcp/service-account-prod.json", true},
		{"config/app-secrets.yaml", true},
		{"config/client_secret.json", true},
		{"helm/secret.yml", true},
		{"kubeconfig", true},
		{"ops/kubeconfig.yaml", true},
		{".npmrc", true},
		{".pypirc", true},
		{".netrc", true},
		{"infra/terraform.tfstate", true},
		{"infra/terraform.tfstate.backup", true},
		{"infra/prod.tfvars", true},
		{"home/.aws/config", true},
		{"home/.ssh/known_hosts", true},
		{".docker/config.json", true},
		{"mcp-oauth-tokens.json", true},
		{"credentials.enc", true},
		{"Credentials.JSON", true},
		// Ordinary source and config stay assessable.
		{"src/auth/jwt.go", false},
		{"config.json", false},
		{"tsconfig.json", false},
		{"internal/secrets/vault.go", false},
		{"docker/config.json", false},
		{"keyboard.go", false},
		{"monkey.py", false},
		{"infra/main.tf", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := SensitivePath(tt.path); got != tt.want {
				t.Errorf("SensitivePath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestSensitiveContent(t *testing.T) {
	filler := strings.Repeat("x := 1\n", 300)
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"rsa private key header", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n", true},
		{"pkcs8 private key header", "key: |\n  -----BEGIN PRIVATE KEY-----\n", true},
		{"openssh private key header", "-----BEGIN OPENSSH PRIVATE KEY-----\n", true},
		{"service account json", `{"type": "service_account", "private_key": "abc"}`, true},
		{"aws secret key", "aws_secret_access_key = abcdEFGH1234\n", true},
		{"github token", "token: ghp_" + strings.Repeat("a", 36) + "\n", true},
		{"openai style key", `API_KEY="sk-` + strings.Repeat("Z", 32) + `"`, true},
		{"slack bot token", "xoxb-1234567890-abcdefghij\n", true},
		{"slack user token", "xoxp-1234567890-abcdefghij\n", true},
		{"public key header is fine", "-----BEGIN PUBLIC KEY-----\n", false},
		{"plain code", "package main\n\nfunc main() {}\n", false},
		{"words containing sk- are fine", "task-runner and disk-usage\n", false},
		{"short sk- prefix is fine", "sk-abc\n", false},
		{"field name without a value is fine", "PrivateKey string `json:\"private_key\"`\n", false},
		{"empty", "", false},
		{"secret on line 300 is checked", strings.Repeat("x\n", 299) + "-----BEGIN PRIVATE KEY-----\n", true},
		{"secret after line 300 is not checked", filler + "-----BEGIN PRIVATE KEY-----\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SensitiveContent(tt.content); got != tt.want {
				t.Errorf("SensitiveContent(%q...) = %v, want %v", truncate(tt.content, 40), got, tt.want)
			}
		})
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func TestAssessFiles_NeverSendsSensitiveFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("assessor called the API for a sensitive file: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &Assessor{apiURL: srv.URL, model: "m", cacheDir: t.TempDir(), concurrency: 1,
		client: &http.Client{Timeout: time.Second}, retryDelay: time.Millisecond}
	files := []FileInput{
		{Path: "credentials.json", Content: `{"user": "a"}`},
		{Path: "deploy/values.yaml", Content: "key: |\n  -----BEGIN PRIVATE KEY-----\n"},
		{Path: "config.json", Content: `{"aws_secret_access_key": "abc"}`},
	}
	res, err := a.AssessFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Errorf("got assessments for sensitive files: %v", res)
	}
}
