package secret

import "testing"

func TestEncryptRoundTrip(t *testing.T) {
	a, err := Encrypt("master-key", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt("master-key", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("nonce was reused")
	}
	plain, err := Decrypt("master-key", a)
	if err != nil || plain != "test-secret" {
		t.Fatalf("round trip: %q %v", plain, err)
	}
	if _, err := Decrypt("wrong-key", a); err == nil {
		t.Fatal("wrong key accepted")
	}
}
