package auth

import (
	"testing"
	"time"
)

func TestSessionsExpireAndLogout(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sessions := NewMemorySessions(5, time.Hour)
	sessions.now = func() time.Time { return now }

	s, err := sessions.Create(Identity{UserID: 1, Subject: "alice"}, "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sessions.Get(s.ID); !ok {
		t.Fatal("session should exist")
	}

	now = now.Add(2 * time.Hour)
	if _, ok := sessions.Get(s.ID); ok {
		t.Fatal("expired session should be rejected")
	}

	s2, _ := sessions.Create(Identity{UserID: 2, Subject: "bob"}, "")
	sessions.Delete(s2.ID)
	if _, ok := sessions.Get(s2.ID); ok {
		t.Fatal("deleted session should be rejected")
	}
}

func TestSessionsEnforcePerUserLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	sessions := NewMemorySessions(2, time.Hour)
	sessions.now = func() time.Time { return now }

	id := Identity{UserID: 1, Subject: "alice"}
	first, _ := sessions.Create(id, "")
	now = now.Add(time.Second)
	second, _ := sessions.Create(id, "")
	now = now.Add(time.Second)
	third, _ := sessions.Create(id, "")

	if _, ok := sessions.Get(first.ID); ok {
		t.Fatal("oldest session should be evicted at limit")
	}
	if _, ok := sessions.Get(second.ID); !ok {
		t.Fatal("second session should remain")
	}
	if _, ok := sessions.Get(third.ID); !ok {
		t.Fatal("new session should exist")
	}

	other, _ := sessions.Create(Identity{UserID: 2, Subject: "bob"}, "")
	if _, ok := sessions.Get(other.ID); !ok {
		t.Fatal("other users must not be affected")
	}
}

func TestDeleteUserRevokesAllSessions(t *testing.T) {
	sessions := NewMemorySessions(5, time.Hour)
	a, _ := sessions.Create(Identity{UserID: 1, Subject: "alice"}, "")
	b, _ := sessions.Create(Identity{UserID: 1, Subject: "alice"}, "")
	c, _ := sessions.Create(Identity{UserID: 2, Subject: "bob"}, "")

	sessions.DeleteUser(1)
	if _, ok := sessions.Get(a.ID); ok {
		t.Fatal("session a should be revoked")
	}
	if _, ok := sessions.Get(b.ID); ok {
		t.Fatal("session b should be revoked")
	}
	if _, ok := sessions.Get(c.ID); !ok {
		t.Fatal("other user session should remain")
	}
}

func TestSafeReturnTo(t *testing.T) {
	cases := map[string]string{
		"":                     "/",
		"/clusters/prod":       "/clusters/prod",
		"//evil.example.com":   "/",
		"https://evil.example": "/",
	}
	for in, want := range cases {
		if got := safeReturnTo(in); got != want {
			t.Fatalf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}
