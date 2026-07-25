package mailcloak

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	appPasswordTimeCost    = 3
	appPasswordMemoryCost  = 65536
	appPasswordParallelism = 1
	appPasswordHashLen     = 32
	appPasswordSaltLen     = 16
)

func HashAppPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("empty password not allowed")
	}

	salt := make([]byte, appPasswordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, appPasswordTimeCost, appPasswordMemoryCost, appPasswordParallelism, appPasswordHashLen)

	enc := base64.RawStdEncoding
	return fmt.Sprintf(
		"{ARGON2ID}$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		appPasswordMemoryCost,
		appPasswordTimeCost,
		appPasswordParallelism,
		enc.EncodeToString(salt),
		enc.EncodeToString(hash),
	), nil
}
