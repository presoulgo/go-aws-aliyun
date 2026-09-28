// Package secret encrypts cloud credentials at rest and manages the keys.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const prefix = "v1:"

// ErrDecrypt means the ciphertext was produced with a different master key or
// was tampered with.
var ErrDecrypt = errors.New("无法解密凭证，主密钥可能已更换，请重新录入 AccessKey")

// Box encrypts and decrypts strings with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// NewBox builds a Box from a 32-byte key.
func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("主密钥长度必须是 32 字节，当前 %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns "v1:" + base64(nonce || ciphertext).
func (b *Box) Encrypt(plain string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(s string) (string, error) {
	if !strings.HasPrefix(s, prefix) {
		return "", ErrDecrypt
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, prefix))
	if err != nil {
		return "", ErrDecrypt
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", ErrDecrypt
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// LoadOrCreateKey returns a 32-byte key. It prefers the configured value
// (base64), then the key file at path, and otherwise generates a key and
// writes it to path with 0600 permissions. created reports the last case.
func LoadOrCreateKey(configured, path string) (key []byte, created bool, err error) {
	if configured != "" {
		key, err := decodeKey(configured)
		if err != nil {
			return nil, false, err
		}
		return key, false, nil
	}
	if raw, err := os.ReadFile(path); err == nil {
		key, err := decodeKey(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, false, fmt.Errorf("密钥文件 %s 无效: %w", path, err)
		}
		return key, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	encoded := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return nil, false, fmt.Errorf("写入密钥文件失败: %w", err)
	}
	return key, true, nil
}

func decodeKey(s string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New("密钥必须是 base64 编码")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("密钥解码后必须是 32 字节，当前 %d", len(key))
	}
	return key, nil
}

// Mask shows the first and last four characters of an access key id.
func Mask(s string) string {
	if utf8.RuneCountInString(s) <= 8 {
		return strings.Repeat("•", 8)
	}
	r := []rune(s)
	return string(r[:4]) + strings.Repeat("•", 8) + string(r[len(r)-4:])
}
