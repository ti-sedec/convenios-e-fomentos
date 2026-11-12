package mtid

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient sobe um X-Via falso em TLS (certificado do httptest) e devolve um cliente que confia só nele.
func newTestClient(t *testing.T, h http.HandlerFunc) *HTTP {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	c, err := NewHTTP(HTTPConfig{BaseURL: srv.URL + "/r1/mt-dev", XRoadClient: "mt-dev:GOV:SEDEC:TESTE", CAFile: caFile})
	require.NoError(t, err)
	return c
}

func TestHTTPFluxoCompleto(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "mt-dev:GOV:SEDEC:TESTE", r.Header.Get("x-road-client"))
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/r1/mt-dev/GOV/MTI/SMARTPASS/file-upload":
			f, hdr, err := r.FormFile("file")
			require.NoError(t, err)
			b, _ := io.ReadAll(f)
			assert.Equal(t, "d.pdf", hdr.Filename)
			assert.Equal(t, "%PDF-x", string(b))
			_, _ = w.Write([]byte(`{"fileId":"F1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/r1/mt-dev/GOV/MTI/SMARTPASS/signature-request":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "12345678910", body["userId"])
			assert.Equal(t, "F1", body["fileId"])
			assert.Equal(t, false, body["detached"])
			assert.Equal(t, map[string]any{"x": float64(200), "y": float64(150)}, body["coordinates"])
			assert.Equal(t, []any{float64(0)}, body["signatureOnPages"])
			_, _ = w.Write([]byte(`{"orderId":"O1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/r1/mt-dev/GOV/MTI/SMARTPASS/signature/O1":
			_, _ = w.Write([]byte(`{"status":"APPROVED","signedIn":1700000000000,"fileId":"F2"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/r1/mt-dev/GOV/MTI/SMARTPASS/file-download/F2":
			_, _ = w.Write([]byte("%PDF-assinado"))
		case r.Method == http.MethodDelete && r.URL.Path == "/r1/mt-dev/GOV/MTI/SMARTPASS/signature/O1":
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	})
	ctx := WithToken(context.Background(), "tok")

	fileID, err := c.Upload(ctx, "d.pdf", []byte("%PDF-x"))
	require.NoError(t, err)
	orderID, err := c.RequestSignature(ctx, Request{SignerCPF: "12345678910", Description: "d", FileID: fileID, X: 200, Y: 150, Pages: []int{0}})
	require.NoError(t, err)
	st, err := c.Status(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, StateApproved, st.State)
	assert.Equal(t, "F2", st.FileID)
	assert.Equal(t, int64(1700000000000), st.SignedAt.UnixMilli())
	data, err := c.Download(ctx, st.FileID)
	require.NoError(t, err)
	assert.Equal(t, "%PDF-assinado", string(data))
	require.NoError(t, c.Cancel(ctx, orderID))

	_, err = c.Status(ctx, "inexistente")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestHTTPSemTokenNaoChamaARede(t *testing.T) {
	c := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("não devia chamar") })
	_, err := c.Status(context.Background(), "O1")
	assert.ErrorIs(t, err, ErrNoToken)
}

func TestHTTPRecusaCertificadoDesconhecido(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	c, err := NewHTTP(HTTPConfig{BaseURL: srv.URL, XRoadClient: "x"}) // sem CAFile: o certificado do servidor não é confiável
	require.NoError(t, err)
	_, err = c.Status(WithToken(context.Background(), "tok"), "O1")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
}

func TestHTTPEstadoDesconhecidoSegueComoPendente(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"WHATEVER"}`)) })
	st, err := c.Status(WithToken(context.Background(), "tok"), "O1")
	require.NoError(t, err)
	assert.Equal(t, StatePending, st.State)
}
