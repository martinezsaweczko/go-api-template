// Package model holds the request and response types shared by the HTTP
// handlers and documented in the OpenAPI spec.
package model

// HealthInfo is the legacy health endpoint response.
type HealthInfo struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// OKResponse is a minimal status response, used by probes and examples.
type OKResponse struct {
	Status string `json:"status"`
}
