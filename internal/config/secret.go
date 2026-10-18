package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateSecret returns a cryptographically random controller secret.
func GenerateSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EnsureSecret sets a random secret when cfg has no non-empty secret. It
// reports whether a new secret was generated.
func EnsureSecret(cfg map[string]any) (bool, error) {
	if cfg == nil {
		return false, nil
	}
	if secret, ok := cfg["secret"].(string); ok && secret != "" {
		return false, nil
	}
	secret, err := GenerateSecret()
	if err != nil {
		return false, err
	}
	cfg["secret"] = secret
	return true, nil
}
