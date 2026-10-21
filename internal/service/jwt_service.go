package service

import (
	"crypto/rsa"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signingMethod is the only algorithm accepted for issuing and verifying tokens.
var signingMethod = jwt.SigningMethodRS256

// Claims represents the JWT claims. The subject is carried by the embedded
// RegisteredClaims, so GetSubject() returns it.
type Claims struct {
	jwt.RegisteredClaims
}

// JWTServiceConfig holds the dependencies and settings for JWTService
type JWTServiceConfig struct {
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
	// Issuer is set on issued tokens and, when non-empty, required on verified tokens.
	Issuer string
	// Audience is set on issued tokens and, when non-empty, required on verified tokens.
	Audience string
	// TTL is the lifetime of issued tokens.
	TTL time.Duration
	Log *slog.Logger
}

// JWTService provides JWT token creation and verification
type JWTService struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	issuer     string
	audience   string
	ttl        time.Duration
	parser     *jwt.Parser
	log        *slog.Logger
}

// NewJWTService creates a new JWT service with the given keys and settings
func NewJWTService(cfg JWTServiceConfig) (*JWTService, error) {
	if cfg.PrivateKey == nil {
		return nil, fmt.Errorf("JWT private key is required")
	}
	if cfg.PublicKey == nil {
		return nil, fmt.Errorf("JWT public key is required")
	}
	if cfg.TTL <= 0 {
		return nil, fmt.Errorf("JWT token TTL must be positive")
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	}
	if cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(cfg.Issuer))
	}
	if cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(cfg.Audience))
	}

	return &JWTService{
		privateKey: cfg.PrivateKey,
		publicKey:  cfg.PublicKey,
		issuer:     cfg.Issuer,
		audience:   cfg.Audience,
		ttl:        cfg.TTL,
		parser:     jwt.NewParser(opts...),
		log:        log,
	}, nil
}

// GenerateToken generates a new JWT token with the given subject
func (s *JWTService) GenerateToken(subject string) (string, error) {
	if subject == "" {
		return "", fmt.Errorf("subject cannot be empty")
	}

	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    s.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	if s.audience != "" {
		claims.Audience = jwt.ClaimStrings{s.audience}
	}

	tokenString, err := jwt.NewWithClaims(signingMethod, claims).SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// VerifyToken verifies a JWT token and returns the claims if valid.
// Only RS256 tokens signed by the configured key, with an expiration and the
// configured issuer and audience, are accepted.
func (s *JWTService) VerifyToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := s.parser.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return s.publicKey, nil
	})
	if err != nil {
		s.log.Debug("Token rejected", "error", err)
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid {
		s.log.Debug("Token rejected", "error", "token not valid")
		return nil, fmt.Errorf("invalid token")
	}

	s.log.Debug("Token verified", "subject", claims.Subject, "issuer", claims.Issuer)
	return claims, nil
}
