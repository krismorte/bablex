package main

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	if got := normalizeEmail("  User@Example.COM "); got != "user@example.com" {
		t.Fatalf("normalizeEmail() = %q", got)
	}
}

func TestValidateCredentials(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		password string
		wantErr  bool
	}{
		{"valid", "user@example.com", "password123", false},
		{"bad email", "user", "password123", true},
		{"short password", "user@example.com", "short", true},
		{"long password", "user@example.com", strings.Repeat("a", 129), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCredentials(tt.email, tt.password); (err != nil) != tt.wantErr {
				t.Fatalf("validateCredentials() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPasswordHashAndVerify(t *testing.T) {
	hash := hashPassword("correct horse battery staple")
	if !verifyPassword("correct horse battery staple", hash) {
		t.Fatal("verifyPassword() rejected a valid password")
	}
	if verifyPassword("wrong password", hash) {
		t.Fatal("verifyPassword() accepted an invalid password")
	}
}

func TestLocalTokenRoundTrip(t *testing.T) {
	a := &app{localAuthSecret: "unit-test-secret"}
	token := a.signToken(jwtClaims{Sub: "user-1", Email: "user@example.com", Type: "access", Exp: time.Now().Add(time.Minute).Unix()})
	claims, ok := a.verifyToken(token, "access", false)
	if !ok || claims.Sub != "user-1" || claims.Email != "user@example.com" {
		t.Fatalf("verifyToken() = %#v, %v", claims, ok)
	}
	if _, ok := a.verifyToken(token+"x", "access", false); ok {
		t.Fatal("verifyToken() accepted a tampered token")
	}
}

func TestBusinessValueNormalizers(t *testing.T) {
	if got := normalizeReaction(" LIKE "); got != "like" {
		t.Fatalf("normalizeReaction() = %q", got)
	}
	if got := normalizeReaction("thumbs-up"); got != "" {
		t.Fatalf("normalizeReaction() invalid = %q", got)
	}
	if got := normalizeDate("2026-09-04"); got != "2026-09-04" {
		t.Fatalf("normalizeDate() = %q", got)
	}
	if got := normalizeDate("04/09/2026"); got != "" {
		t.Fatalf("normalizeDate() invalid = %q", got)
	}
	for _, status := range []string{"owned", "reading", "read", "wishlist", "lent", "sold", "donated"} {
		if !validStatus(status) {
			t.Fatalf("validStatus(%q) = false", status)
		}
	}
	if validStatus("unknown") {
		t.Fatal("validStatus(unknown) = true")
	}
}

func TestCurrentBookValidation(t *testing.T) {
	good := Book{ID: "book-1", Title: "A book", CreatedByID: "user-1"}
	if !isCurrentBook(good) {
		t.Fatal("isCurrentBook() rejected a valid book")
	}
	for _, bad := range []Book{
		{ID: "book-1", Title: "", CreatedByID: "user-1"},
		{ID: "book-1", Title: "A book", CreatedByID: ""},
		{ID: "", Title: "A book", CreatedByID: "user-1"},
	} {
		if isCurrentBook(bad) {
			t.Fatalf("isCurrentBook() accepted invalid book: %#v", bad)
		}
	}
}

func TestPreferredLanguageNormalization(t *testing.T) {
	tests := map[string]string{"": "en", "en": "en", "ES": "es", " pt ": "pt", "fr": "fr", "de": "de", "xx": "en"}
	for input, want := range tests {
		if got := normalizePreferredLanguage(input); got != want {
			t.Fatalf("normalizePreferredLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBookLanguageNormalization(t *testing.T) {
	for _, input := range []string{"en", "ES", "pt", "fr", "de"} {
		if got := normalizeBookLanguage(input); got == "" {
			t.Fatalf("normalizeBookLanguage(%q) returned empty", input)
		}
	}
	if got := normalizeBookLanguage("xx"); got != "" {
		t.Fatalf("normalizeBookLanguage(invalid) = %q", got)
	}
}

func TestSplitPath(t *testing.T) {
	got := splitPath("/libraries/lib-1/books/book-1/")
	want := []string{"libraries", "lib-1", "books", "book-1"}
	if len(got) != len(want) {
		t.Fatalf("splitPath() = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitPath() = %#v, want %#v", got, want)
		}
	}
}

func TestCommentKeys(t *testing.T) {
	if got := commentPartitionKey(" book-1 "); got != "BOOK#book-1" {
		t.Fatalf("commentPartitionKey() = %q", got)
	}
	if got := commentSortKey("2026-09-04T10:00:00Z", "comment-1"); got != "COMMENT#2026-09-04T10:00:00Z#comment-1" {
		t.Fatalf("commentSortKey() = %q", got)
	}
}

func TestSourceIPAllowed(t *testing.T) {
	t.Setenv("ALLOWED_SOURCE_CIDRS", "203.0.113.10/32,198.51.100.0/24")
	if !sourceIPAllowed("203.0.113.10") {
		t.Fatal("sourceIPAllowed() rejected the configured personal IP")
	}
	if !sourceIPAllowed("198.51.100.42") {
		t.Fatal("sourceIPAllowed() rejected an address in the configured CIDR")
	}
	if sourceIPAllowed("192.0.2.10") {
		t.Fatal("sourceIPAllowed() accepted an address outside the allowlist")
	}
	if sourceIPAllowed("not-an-ip") {
		t.Fatal("sourceIPAllowed() accepted an invalid IP")
	}
}
