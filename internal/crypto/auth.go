package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
	tokenBytes          = 32
)

var (
	ErrInvalidHash      = errors.New("auth: argon2 hash has invalid format")
	ErrIncompatibleVer  = errors.New("auth: incompatible argon2 version")
	ErrPasswordMismatch = errors.New("auth: password does not match")
)

type argon2Params struct {
	memory, time uint32
	threads      uint8
	keyLen       uint32
}

// HashPassword hashes password with Argon2id and returns a storable encoded string.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generating salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword checks password against encodedHash; returns nil on match.
func VerifyPassword(encodedHash, password string) error {
	p, salt, hash, err := decodeHash(encodedHash)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(hash, argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}

var burnSalt = make([]byte, argonSaltLen)

// BurnPasswordCheck spends the same Argon2id work as VerifyPassword and discards the result.
func BurnPasswordCheck(password string) {
	argon2.IDKey([]byte(password), burnSalt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

func decodeHash(encoded string) (p argon2Params, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		if err == nil {
			err = ErrIncompatibleVer
		} else {
			err = ErrInvalidHash
		}
		return p, nil, nil, err
	}
	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if hash, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	p.keyLen = uint32(len(hash))
	return p, salt, hash, nil
}

// GenerateToken returns a random URL-safe token and its SHA-256 hex digest for storage.
func GenerateToken() (raw, hashed string, err error) {
	b := make([]byte, tokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("auth: generating token: %w", err)
	}
	raw = base64.URLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken returns the SHA-256 hex digest of a raw token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
