package model

// TokenRequest represents the request payload for token creation
type TokenRequest struct {
	Subject string `json:"subject"`
}

// TokenResponse represents the response payload with the generated token
type TokenResponse struct {
	Token string `json:"token"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}
