package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
)

func TestCabecalhosDeSeguranca(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/health", "/api/admin/roles", "/nao-existe"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		req.RemoteAddr = "10.0.0.1:1"
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		h := rec.Header()
		assert.Equal(t, "no-store", h.Get("Cache-Control"), path)
		assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), path)
		assert.Equal(t, "DENY", h.Get("X-Frame-Options"), path)
		assert.Equal(t, "no-referrer", h.Get("Referrer-Policy"), path)
		assert.Contains(t, h.Get("Content-Security-Policy"), "default-src 'none'", path)
		assert.NotEmpty(t, h.Get("Strict-Transport-Security"), path)
		assert.NotEmpty(t, h.Get("X-Request-ID"), path)
		assert.Contains(t, h.Get("Content-Type"), "application/json", path)
	}
}

func TestLimiteGeralPorIP(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RateLimitGlobalPerMin = 5 })
	for range 5 {
		assert.Equal(t, 200, e.do("GET", "/api/health", nil, "").Status)
	}
	r := e.do("GET", "/api/health", nil, "")
	assert.Equal(t, 429, r.Status)
	assert.Equal(t, "rate_limited", r.errCode())
	// Outro IP tem contagem própria.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "10.0.0.99:1"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	assert.Equal(t, 200, rec.Code)
}
