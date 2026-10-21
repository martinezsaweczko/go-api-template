// Package repository is the home for persistence adapters: implementations
// of the storage ports the service layer depends on (databases, caches,
// external stores). Keep driver-specific code here so that internal/service
// stays independent of any particular storage technology.
package repository
