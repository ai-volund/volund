package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// Generate a random 32-byte key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	keyHex := hex.EncodeToString(key)

	broker, err := NewBroker(nil, keyHex)
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	plaintext := []byte("ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	ciphertext, err := broker.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if string(ciphertext) == string(plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	decrypted, err := broker.decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestNewBrokerBadKey(t *testing.T) {
	// Too short.
	_, err := NewBroker(nil, "aabbccdd")
	if err == nil {
		t.Fatal("expected error for short key")
	}

	// Not hex.
	_, err = NewBroker(nil, "not-hex-at-all-not-hex-at-all-not-hex-at-all-not-hex-at-all-1234")
	if err == nil {
		t.Fatal("expected error for non-hex key")
	}
}

func TestIntersectScopes(t *testing.T) {
	tests := []struct {
		requested []string
		available []string
		want      int
	}{
		{nil, []string{"read", "write"}, 2},              // empty requested = all available
		{[]string{"read"}, []string{"read", "write"}, 1}, // subset
		{[]string{"admin"}, []string{"read", "write"}, 0}, // no overlap
		{[]string{"read", "write"}, []string{"read"}, 1},  // partial overlap
	}

	for _, tt := range tests {
		got := intersectScopes(tt.requested, tt.available)
		if len(got) != tt.want {
			t.Errorf("intersectScopes(%v, %v) = %v (len %d), want len %d",
				tt.requested, tt.available, got, len(got), tt.want)
		}
	}
}
