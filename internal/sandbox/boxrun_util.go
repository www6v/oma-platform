package sandbox

import (
	"encoding/base64"
	"fmt"
)

func decodeBase64(s string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	return string(raw), nil
}
