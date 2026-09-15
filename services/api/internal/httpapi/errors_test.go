package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// retryable is a promise to the client. Getting it wrong on a permanent
// condition turns a clear failure into an infinite retry loop.
func TestRetryableIsFalseForPermanentConditions(t *testing.T) {
	cases := map[int]bool{
		http.StatusBadRequest:          false,
		http.StatusUnauthorized:        false,
		http.StatusForbidden:           false,
		http.StatusNotFound:            false,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable:  true,
		http.StatusGatewayTimeout:      true,
		// A feature that is switched off will still be off on a retry.
		http.StatusNotImplemented: false,
	}

	for status, want := range cases {
		rec := httptest.NewRecorder()
		WriteError(rec, httptest.NewRequest(http.MethodGet, "/", nil),
			status, "CODE", "message", nil)

		var body struct {
			Error struct {
				RetryAble bool `json:"retryable"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("status %d: decode: %v", status, err)
		}
		if body.Error.RetryAble != want {
			t.Errorf("status %d: retryable = %v, want %v", status, body.Error.RetryAble, want)
		}
	}
}
