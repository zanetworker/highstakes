package llm

import (
	"path"
	"regexp"
	"strings"
)

// sensitiveNames are file names (lowercased) that hold credentials.
var sensitiveNames = map[string]bool{
	"credentials.json": true, "auth.json": true, "mcp-oauth-tokens.json": true,
	"credentials.enc": true, ".npmrc": true, ".pypirc": true, ".netrc": true,
}

// sensitiveGlobs match lowercased file names that hold credentials.
var sensitiveGlobs = []string{
	".env", ".env.*", "*.pem", "*.key", "id_rsa*", "id_dsa*", "id_ecdsa*", "id_ed25519*",
	"service-account*.json", "*secret*.json", "*secret*.yaml", "*secret*.yml",
	"kubeconfig", "kubeconfig.*", "terraform.tfstate*", "*.tfvars",
}

// SensitivePath reports whether a repo-relative path names a file that
// likely holds credentials, so it must never be sent to the LLM.
func SensitivePath(p string) bool {
	p = strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return false
	}
	base := path.Base(p)
	if sensitiveNames[base] {
		return true
	}
	for _, g := range sensitiveGlobs {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	dirs := strings.Split(path.Dir(p), "/")
	for _, d := range dirs {
		if d == ".aws" || d == ".ssh" {
			return true
		}
	}
	return base == "config.json" && len(dirs) > 0 && dirs[len(dirs)-1] == ".docker"
}

// secretPatterns are private key headers and token shapes. Each needs a
// value next to the marker so code that only names the field does not match.
var secretPatterns = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----` +
	`|"private_key"\s*:` +
	`|(?i:aws_secret_access_key)"?\s*[=:]` +
	`|\bghp_[A-Za-z0-9]{36}` +
	`|\bsk-[A-Za-z0-9_-]{20,}` +
	`|\bxox[bp]-[A-Za-z0-9-]{10,}`)

// secretScanLines matches the prompt cutoff: only lines the LLM would see.
const secretScanLines = 300

// SensitiveContent reports whether the first 300 lines of content contain a
// private key or an obvious token, so the file must never be sent to the LLM.
func SensitiveContent(content string) bool {
	lines := strings.SplitN(content, "\n", secretScanLines+1)
	if len(lines) > secretScanLines {
		lines = lines[:secretScanLines]
	}
	return secretPatterns.MatchString(strings.Join(lines, "\n"))
}
