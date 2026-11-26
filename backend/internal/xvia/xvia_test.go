package xvia

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newReq(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	return req
}

func TestNewHTTPClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	good := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(good, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))

	t.Run("confia no certificado informado", func(t *testing.T) {
		c, err := NewHTTPClient(TLS{CAFile: good, Timeout: 5 * time.Second})
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, c.Timeout)
		res, err := c.Do(newReq(t, srv.URL))
		require.NoError(t, err)
		_ = res.Body.Close()
	})
	t.Run("sem o certificado a verificação continua ligada", func(t *testing.T) {
		c, err := NewHTTPClient(TLS{})
		require.NoError(t, err)
		assert.Equal(t, 30*time.Second, c.Timeout, "padrão de 30 s")
		res, err := c.Do(newReq(t, srv.URL))
		if res != nil {
			_ = res.Body.Close()
		}
		assert.Error(t, err)
	})
	t.Run("arquivo inexistente ou sem PEM", func(t *testing.T) {
		_, err := NewHTTPClient(TLS{CAFile: filepath.Join(dir, "nao-existe.pem")})
		assert.Error(t, err)
		bad := filepath.Join(dir, "lixo.pem")
		require.NoError(t, os.WriteFile(bad, []byte("não é PEM"), 0o600))
		_, err = NewHTTPClient(TLS{CAFile: bad})
		assert.Error(t, err)
	})
}
