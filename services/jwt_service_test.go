package services

import (
	"crypto/rand"
	"crypto/rsa"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer   = "go-api-template"
	testAudience = "go-api-template-clients"
)

func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func newTestService(t *testing.T, key *rsa.PrivateKey, issuer, audience string) *JWTService {
	t.Helper()
	svc, err := NewJWTService(JWTServiceConfig{
		PrivateKey: key,
		PublicKey:  &key.PublicKey,
		Issuer:     issuer,
		Audience:   audience,
		TTL:        time.Hour,
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewJWTService: %v", err)
	}
	return svc
}

func signRSA(t *testing.T, method jwt.SigningMethod, key *rsa.PrivateKey, claims jwt.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

func TestJWTService_GenerateAndVerify(t *testing.T) {
	key := newTestKey(t)
	svc := newTestService(t, key, testIssuer, testAudience)

	token, err := svc.GenerateToken("user-123")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := svc.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}

	subject, err := claims.GetSubject()
	if err != nil {
		t.Fatalf("GetSubject: %v", err)
	}
	if subject != "user-123" {
		t.Errorf("GetSubject() = %q, want %q", subject, "user-123")
	}
	if claims.Issuer != testIssuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, testIssuer)
	}
	if claims.ExpiresAt == nil {
		t.Error("ExpiresAt is nil, want an expiration")
	}
}

func TestJWTService_GenerateToken_EmptySubject(t *testing.T) {
	svc := newTestService(t, newTestKey(t), testIssuer, testAudience)
	if _, err := svc.GenerateToken(""); err == nil {
		t.Fatal("GenerateToken(\"\") error = nil, want error")
	}
}

func TestJWTService_VerifyToken_Rejects(t *testing.T) {
	key := newTestKey(t)
	otherKey := newTestKey(t)
	svc := newTestService(t, key, testIssuer, testAudience)

	now := time.Now()
	valid := func() jwt.RegisteredClaims {
		return jwt.RegisteredClaims{
			Subject:   "user-123",
			Issuer:    testIssuer,
			Audience:  jwt.ClaimStrings{testAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}
	}

	tests := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{
			name: "RS512 algorithm",
			token: func(t *testing.T) string {
				return signRSA(t, jwt.SigningMethodRS512, key, valid())
			},
		},
		{
			name: "HS256 algorithm",
			token: func(t *testing.T) string {
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, valid()).SignedString([]byte("shared-secret"))
				if err != nil {
					t.Fatalf("sign HS256: %v", err)
				}
				return token
			},
		},
		{
			name: "none algorithm",
			token: func(t *testing.T) string {
				token, err := jwt.NewWithClaims(jwt.SigningMethodNone, valid()).SignedString(jwt.UnsafeAllowNoneSignatureType)
				if err != nil {
					t.Fatalf("sign none: %v", err)
				}
				return token
			},
		},
		{
			name: "signed with another key",
			token: func(t *testing.T) string {
				return signRSA(t, jwt.SigningMethodRS256, otherKey, valid())
			},
		},
		{
			name: "expired",
			token: func(t *testing.T) string {
				c := valid()
				c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
				return signRSA(t, jwt.SigningMethodRS256, key, c)
			},
		},
		{
			name: "missing expiration",
			token: func(t *testing.T) string {
				c := valid()
				c.ExpiresAt = nil
				return signRSA(t, jwt.SigningMethodRS256, key, c)
			},
		},
		{
			name: "wrong issuer",
			token: func(t *testing.T) string {
				c := valid()
				c.Issuer = "someone-else"
				return signRSA(t, jwt.SigningMethodRS256, key, c)
			},
		},
		{
			name: "wrong audience",
			token: func(t *testing.T) string {
				c := valid()
				c.Audience = jwt.ClaimStrings{"another-api"}
				return signRSA(t, jwt.SigningMethodRS256, key, c)
			},
		},
		{
			name: "missing audience",
			token: func(t *testing.T) string {
				c := valid()
				c.Audience = nil
				return signRSA(t, jwt.SigningMethodRS256, key, c)
			},
		},
		{
			name: "malformed",
			token: func(t *testing.T) string {
				return "not-a-jwt"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.VerifyToken(tt.token(t)); err == nil {
				t.Fatal("VerifyToken() error = nil, want error")
			}
		})
	}
}

func TestJWTService_VerifyToken_AudienceOptional(t *testing.T) {
	key := newTestKey(t)
	svc := newTestService(t, key, testIssuer, "")

	token, err := svc.GenerateToken("user-123")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, err := svc.VerifyToken(token); err != nil {
		t.Fatalf("VerifyToken() error = %v, want nil when no audience is configured", err)
	}
}

func TestNewJWTService_Validation(t *testing.T) {
	key := newTestKey(t)

	tests := []struct {
		name string
		cfg  JWTServiceConfig
	}{
		{name: "missing private key", cfg: JWTServiceConfig{PublicKey: &key.PublicKey, TTL: time.Hour}},
		{name: "missing public key", cfg: JWTServiceConfig{PrivateKey: key, TTL: time.Hour}},
		{name: "non-positive TTL", cfg: JWTServiceConfig{PrivateKey: key, PublicKey: &key.PublicKey}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewJWTService(tt.cfg); err == nil {
				t.Fatal("NewJWTService() error = nil, want error")
			}
		})
	}
}
