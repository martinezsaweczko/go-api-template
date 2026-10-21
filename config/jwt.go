package config

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

const (
	// DefaultJWTIssuer is the issuer set on and required from tokens when none is configured.
	DefaultJWTIssuer = "go-api-template"
	// DefaultJWTTokenTTL is the lifetime of issued tokens when none is configured.
	DefaultJWTTokenTTL = time.Hour
)

type JWTConfig struct {
	PublicKeyPath  string
	PrivateKeyPath string
	PublicKey      *rsa.PublicKey
	PrivateKey     *rsa.PrivateKey
	// Issuer is set on issued tokens and required on verified tokens.
	Issuer string
	// Audience, when non-empty, is set on issued tokens and required on verified tokens.
	Audience string
	// TokenTTL is the lifetime of issued tokens.
	TokenTTL time.Duration
	// DevTokenEndpoint enables the unauthenticated token minting endpoint.
	// Development only: never enable it in production.
	DevTokenEndpoint bool
}

func (j *JWTConfig) validate() error {
	if j.PublicKeyPath == "" {
		return fmt.Errorf("JWT public key path cannot be empty")
	}
	if j.PrivateKeyPath == "" {
		return fmt.Errorf("JWT private key path cannot be empty")
	}
	if j.TokenTTL <= 0 {
		return fmt.Errorf("JWT token TTL must be positive")
	}

	// Verify files exist
	if _, err := os.Stat(j.PublicKeyPath); err != nil {
		return fmt.Errorf("JWT public key file not found: %s", j.PublicKeyPath)
	}
	if _, err := os.Stat(j.PrivateKeyPath); err != nil {
		return fmt.Errorf("JWT private key file not found: %s", j.PrivateKeyPath)
	}

	return nil
}

// LoadKeys loads the public and private keys from files
func (j *JWTConfig) LoadKeys() error {
	if err := j.validate(); err != nil {
		return err
	}

	// Read and parse private key
	privateKeyBytes, err := os.ReadFile(j.PrivateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key file: %w", err)
	}

	privateKey, err := parsePrivateKey(privateKeyBytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}
	j.PrivateKey = privateKey

	// Read and parse public key
	publicKeyBytes, err := os.ReadFile(j.PublicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key file: %w", err)
	}

	publicKey, err := parsePublicKey(publicKeyBytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}
	j.PublicKey = publicKey

	return nil
}

// parsePrivateKey parses a PEM-encoded RSA private key (supports both PKCS#1 and PKCS#8 formats)
func parsePrivateKey(keyBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid private key format")
	}

	// Try PKCS#1 format first
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		return privateKey, nil
	}

	// If PKCS#1 fails, try PKCS#8 format
	pkcs8Key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key in PKCS#1 or PKCS#8 format: %w", err)
	}

	rsaKey, ok := pkcs8Key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not an RSA key")
	}

	return rsaKey, nil
}

// parsePublicKey parses a PEM-encoded RSA public key
func parsePublicKey(keyBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid public key format")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	publicKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return publicKey, nil
}
