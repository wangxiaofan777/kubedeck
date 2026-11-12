package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func mustRing(t *testing.T, current int, keys map[int][]byte) *Ring {
	t.Helper()
	r, err := NewRing(current, keys)
	if err != nil {
		t.Fatalf("NewRing: %v", err)
	}
	return r
}

func testKey(b byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return key
}

func TestSealOpenRoundTrip(t *testing.T) {
	r := mustRing(t, 1, map[int][]byte{1: testKey(1)})
	plaintext := []byte("apiVersion: v1\nkind: Config\nclusters: []\n")

	sealed, err := r.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed.KeyVersion != 1 {
		t.Fatalf("KeyVersion = %d, want 1", sealed.KeyVersion)
	}
	if len(sealed.Nonce) != 12 {
		t.Fatalf("nonce size = %d, want 12", len(sealed.Nonce))
	}
	if string(sealed.Ciphertext) == string(plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := r.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("plaintext = %q, want %q", got, plaintext)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	r := mustRing(t, 1, map[int][]byte{1: testKey(1)})
	a, _ := r.Seal([]byte("same"))
	b, _ := r.Seal([]byte("same"))
	if string(a.Nonce) == string(b.Nonce) {
		t.Fatal("two seals must not reuse the same nonce")
	}
	if string(a.Ciphertext) == string(b.Ciphertext) {
		t.Fatal("same plaintext sealed twice must produce different ciphertext")
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	r := mustRing(t, 1, map[int][]byte{1: testKey(1)})
	sealed, _ := r.Seal([]byte("secret"))
	sealed.Ciphertext[0] ^= 0xff

	if _, err := r.Open(sealed); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("Open error = %v, want ErrDecrypt", err)
	}
}

func TestOpenRejectsWrongKeyAndUnknownVersion(t *testing.T) {
	r := mustRing(t, 1, map[int][]byte{1: testKey(1)})
	sealed, _ := r.Seal([]byte("secret"))

	t.Run("unknown version", func(t *testing.T) {
		bad := sealed
		bad.KeyVersion = 9
		if _, err := r.Open(bad); !errors.Is(err, ErrUnknownKeyVersion) {
			t.Fatalf("Open error = %v, want ErrUnknownKeyVersion", err)
		}
	})

	t.Run("same version but different key", func(t *testing.T) {
		other := mustRing(t, 1, map[int][]byte{1: testKey(2)})
		if _, err := other.Open(sealed); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("Open error = %v, want ErrDecrypt", err)
		}
	})

	t.Run("version downgrade changes aad", func(t *testing.T) {
		// 同一把密钥，但把 key_version 改成 2：AAD 不同，认证必须失败。
		ring := mustRing(t, 2, map[int][]byte{1: testKey(1), 2: testKey(1)})
		bad := sealed
		bad.KeyVersion = 2
		if _, err := ring.Open(bad); !errors.Is(err, ErrDecrypt) {
			t.Fatalf("Open error = %v, want ErrDecrypt", err)
		}
	})
}

func TestRotate(t *testing.T) {
	v1 := testKey(1)
	v2 := testKey(2)
	r := mustRing(t, 2, map[int][]byte{1: v1, 2: v2})

	old, _ := r.sealWith(1, []byte("kubeconfig"))
	if !r.NeedsRotation(old.KeyVersion) {
		t.Fatal("v1 ciphertext should need rotation when current is v2")
	}

	rotated, err := r.Rotate(old)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rotated.KeyVersion != 2 {
		t.Fatalf("rotated key version = %d, want 2", rotated.KeyVersion)
	}
	if string(rotated.Ciphertext) == string(old.Ciphertext) {
		t.Fatal("rotation must re-encrypt the payload")
	}
	got, err := r.Open(rotated)
	if err != nil {
		t.Fatalf("Open rotated: %v", err)
	}
	if string(got) != "kubeconfig" {
		t.Fatalf("rotated plaintext = %q", got)
	}
	if r.NeedsRotation(rotated.KeyVersion) {
		t.Fatal("rotated ciphertext should not need another rotation")
	}
}

func TestRotateFailsWhenOldKeyMissing(t *testing.T) {
	r := mustRing(t, 2, map[int][]byte{2: testKey(2)})
	// 用一次性密钥环制造一条 v1 密文。
	oldRing := mustRing(t, 1, map[int][]byte{1: testKey(1)})
	old, _ := oldRing.Seal([]byte("x"))

	if _, err := r.Rotate(old); !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("Rotate error = %v, want ErrUnknownKeyVersion", err)
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint([]byte("token-a"))
	b := Fingerprint([]byte("token-a"))
	c := Fingerprint([]byte("token-b"))

	if a != b {
		t.Fatal("fingerprint must be deterministic")
	}
	if a == c {
		t.Fatal("different payloads must have different fingerprints")
	}
	if len(a) != 32 || strings.ContainsAny(a, "ghijklmnopqrstuvwxyz") {
		t.Fatalf("fingerprint %q should be 32 hex chars", a)
	}
}

func TestNewRingValidation(t *testing.T) {
	tests := []struct {
		name    string
		current int
		keys    map[int][]byte
	}{
		{"empty ring", 1, nil},
		{"short key", 1, map[int][]byte{1: make([]byte, 16)}},
		{"zero version", 1, map[int][]byte{0: testKey(0), 1: testKey(1)}},
		{"current missing", 3, map[int][]byte{1: testKey(1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRing(tc.current, tc.keys); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestParseKey(t *testing.T) {
	raw := testKey(7)
	encoded := base64.StdEncoding.EncodeToString(raw)

	got, err := ParseKey(encoded)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	if string(got) != string(raw) {
		t.Fatal("ParseKey round-trip mismatch")
	}

	for _, bad := range []string{"", "not-base64!!", base64.StdEncoding.EncodeToString(make([]byte, 8))} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatalf("ParseKey(%q) expected error", bad)
		}
	}
}
