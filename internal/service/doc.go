// Package service holds the application's business logic, independent of the
// HTTP transport and of storage. It currently provides JWTService, which
// issues and verifies RS256 tokens. Persistence adapters live in
// internal/repository and are injected into services through interfaces.
package service
