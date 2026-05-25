package services

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// ProviderError wraps provider-specific errors with classification
type ProviderError struct {
	Kind    ProviderErrorKind
	Message string
	Cause   error
}

type ProviderErrorKind int

const (
	ErrKindAuth      ProviderErrorKind = iota // credentials expired/invalid
	ErrKindRateLimit                          // rate limited by upstream
	ErrKindNetwork                            // transient network failure
	ErrKindConfig                             // invalid configuration
	ErrKindUpstream                           // upstream API error (non-transient)
)

func (e *ProviderError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ProviderError) Unwrap() error { return e.Cause }

func NewProviderError(kind ProviderErrorKind, msg string, cause error) *ProviderError {
	return &ProviderError{Kind: kind, Message: msg, Cause: cause}
}

// classifyHTTPStatus returns a ProviderError for a non-200 HTTP status code.
func classifyHTTPStatus(statusCode int, provider string) *ProviderError {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return NewProviderError(ErrKindAuth,
			fmt.Sprintf("%s api returned status: %d", provider, statusCode), nil)
	case statusCode == http.StatusTooManyRequests:
		return NewProviderError(ErrKindRateLimit,
			fmt.Sprintf("%s api returned status: %d", provider, statusCode), nil)
	case statusCode >= 400 && statusCode < 500:
		return NewProviderError(ErrKindConfig,
			fmt.Sprintf("%s api returned status: %d", provider, statusCode), nil)
	default:
		return NewProviderError(ErrKindUpstream,
			fmt.Sprintf("%s api returned status: %d", provider, statusCode), nil)
	}
}

// classifyNetworkError wraps a transport-level error as a ProviderError.
func classifyNetworkError(err error, provider string) *ProviderError {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return NewProviderError(ErrKindNetwork,
			fmt.Sprintf("%s network error", provider), err)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return NewProviderError(ErrKindNetwork,
			fmt.Sprintf("%s network error", provider), err)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return NewProviderError(ErrKindNetwork,
			fmt.Sprintf("%s network error", provider), err)
	}

	if errors.Is(err, os.ErrDeadlineExceeded) {
		return NewProviderError(ErrKindNetwork,
			fmt.Sprintf("%s request timed out", provider), err)
	}

	if isConnectionError(err) {
		return NewProviderError(ErrKindNetwork,
			fmt.Sprintf("%s network error", provider), err)
	}

	return NewProviderError(ErrKindNetwork,
		fmt.Sprintf("%s request failed", provider), err)
}

func isConnectionError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout")
}
