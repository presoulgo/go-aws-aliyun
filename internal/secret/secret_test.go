package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoxRoundTripAndTamper(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	box, err := NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.Encrypt("super-secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, "super-secret") || !strings.HasPrefix(enc, "v1:") {
		t.Fatalf("ciphertext looks wrong: %s", enc)
	}
	enc2, _ := box.Encrypt("super-secret")
	if enc == enc2 {
		t.Fatal("nonce must make ciphertexts differ")
	}
	plain, err := box.Decrypt(enc)
	if err != nil || plain != "super-secret" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}

	other, _ := NewBox(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Decrypt(enc); err != ErrDecrypt {
		t.Fatalf("expected ErrDecrypt with other key, got %v", err)
	}
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := box.Decrypt(tampered); err != ErrDecrypt {
		t.Fatalf("expected ErrDecrypt for tampered data, got %v", err)
	}
}

func TestLoadOrCreateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "master.key")
	k1, created, err := LoadOrCreateKey("", path)
	if err != nil || !created || len(k1) != 32 {
		t.Fatalf("first load: %v %v %d", err, created, len(k1))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v", info.Mode().Perm())
	}
	k2, created, err := LoadOrCreateKey("", path)
	if err != nil || created || !bytes.Equal(k1, k2) {
		t.Fatal("second load must reuse the file")
	}
	if _, _, err := LoadOrCreateKey("not-base64!!", path); err == nil {
		t.Fatal("expected error for invalid configured key")
	}
}

func TestMask(t *testing.T) {
	if got := Mask("AKIAIOSFODNN7EXAMPLE"); got != "AKIA••••••••MPLE" {
		t.Fatalf("mask = %s", got)
	}
	if got := Mask("short"); got != "••••••••" {
		t.Fatalf("mask short = %s", got)
	}
}
