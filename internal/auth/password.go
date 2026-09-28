// Package auth implements password hashing, login tokens and login lockout.
package auth

import (
	"crypto/rand"
	"errors"
	"math/big"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is the minimum accepted password length.
const MinPasswordLength = 10

// HashPassword hashes a password with bcrypt.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword reports whether password matches the bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ValidatePassword enforces the password policy: at least 10 characters,
// containing both letters and digits.
func ValidatePassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return errors.New("密码至少 10 位")
	}
	if len(password) > 72 {
		return errors.New("密码不能超过 72 个字节")
	}
	var letter, digit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		}
	}
	if !letter || !digit {
		return errors.New("密码需要同时包含字母和数字")
	}
	return nil
}

// Characters that are hard to confuse when a password is read aloud or copied.
const (
	pwLetters = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"
	pwDigits  = "23456789"
)

// GeneratePassword returns a random password of length n (minimum 10) that
// satisfies ValidatePassword.
func GeneratePassword(n int) (string, error) {
	if n < MinPasswordLength {
		n = MinPasswordLength
	}
	all := pwLetters + pwDigits
	for {
		out := make([]byte, n)
		for i := range out {
			idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(all))))
			if err != nil {
				return "", err
			}
			out[i] = all[idx.Int64()]
		}
		if ValidatePassword(string(out)) == nil {
			return string(out), nil
		}
	}
}
