package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

func Encrypt(key, plaintext string) (string, error) {
	if key == "" {
		return "", errors.New("OPS_SECRET_KEY is required")
	}
	h := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(h[:])
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

func Decrypt(key, encoded string) (string, error) {
	h := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(h[:])
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(b) < g.NonceSize() {
		return "", errors.New("invalid secret")
	}
	p, err := g.Open(nil, b[:g.NonceSize()], b[g.NonceSize():], nil)
	return string(p), err
}
