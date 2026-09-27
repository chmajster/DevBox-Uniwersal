package databases

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

func GeneratePassword() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate database password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func safeNamePart(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else if b.Len() > 0 && b.String()[b.Len()-1] != '_' {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
