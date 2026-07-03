package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const exampleCom = "http://example.com"

func TestWithCORS(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins []string
		originHeader   string
		expectedCORS   string
	}{
		{
			name:           "Empty allowed origins",
			allowedOrigins: []string{},
			originHeader:   exampleCom,
			expectedCORS:   "",
		},
		{
			name:           "Allowed origin matches",
			allowedOrigins: []string{"http://localhost:5173", exampleCom},
			originHeader:   exampleCom,
			expectedCORS:   exampleCom,
		},
		{
			name:           "Allowed origin does not match",
			allowedOrigins: []string{"http://localhost:5173"},
			originHeader:   exampleCom,
			expectedCORS:   "",
		},
		{
			name:           "Wildcard matches any",
			allowedOrigins: []string{"*"},
			originHeader:   "http://another.com",
			expectedCORS:   "http://another.com",
		},
		{
			name:           "No origin header in request",
			allowedOrigins: []string{"*"},
			originHeader:   "",
			expectedCORS:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			handler := withCORS(tt.allowedOrigins, dummyHandler)
			req := httptest.NewRequest("POST", "/test", nil)
			if tt.originHeader != "" {
				req.Header.Set("Origin", tt.originHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			corsHeader := rec.Header().Get("Access-Control-Allow-Origin")
			if corsHeader != tt.expectedCORS {
				t.Errorf("expected Access-Control-Allow-Origin %q, got %q", tt.expectedCORS, corsHeader)
			}
		})
	}
}
