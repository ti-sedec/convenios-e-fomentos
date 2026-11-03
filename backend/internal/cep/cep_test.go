package cep

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"78005-100": "78005100", "78005100": "78005100", " 78.005-100 ": "78005100"} {
		got, err := Normalize(in)
		assert.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "1234567", "123456789", "7800510A", "78005/100"} {
		_, err := Normalize(in)
		assert.ErrorIs(t, err, ErrInvalid, in)
	}
}

// fakeViaCEP responde como o ViaCEP e conta as chamadas.
func fakeViaCEP(t *testing.T) (*Service, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/78005100/json/":
			_, _ = w.Write([]byte(`{"cep":"78005-100","logradouro":"Rua Barão de Melgaço","bairro":"Centro","localidade":"Cuiabá","uf":"mt","ibge":"5103403"}`))
		case "/99999999/json/":
			_, _ = w.Write([]byte(`{"erro":"true"}`))
		case "/00000000/json/":
			_, _ = w.Write([]byte(`{"erro":true}`))
		case "/11111111/json/":
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return NewService(srv.URL, time.Second), &calls
}

func TestLookup(t *testing.T) {
	svc, _ := fakeViaCEP(t)
	a, err := svc.Lookup(context.Background(), "78005-100")
	require.NoError(t, err)
	assert.Equal(t, &Address{Zip: "78005100", Street: "Rua Barão de Melgaço", District: "Centro", City: "Cuiabá", UF: "MT", IBGECode: "5103403"}, a)
}

func TestLookupErros(t *testing.T) {
	svc, _ := fakeViaCEP(t)
	ctx := context.Background()
	_, err := svc.Lookup(ctx, "123")
	assert.ErrorIs(t, err, ErrInvalid)
	for _, zip := range []string{"99999999", "00000000", "11111111"} {
		_, err = svc.Lookup(ctx, zip)
		assert.ErrorIs(t, err, ErrNotFound, zip)
	}
	_, err = svc.Lookup(ctx, "22222222") // 500 do ViaCEP
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestLookupIndisponivel(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // ninguém escutando
	_, err := NewService(srv.URL, 200*time.Millisecond).Lookup(context.Background(), "78005100")
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestLookupCache(t *testing.T) {
	svc, calls := fakeViaCEP(t)
	now := time.Now()
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	for range 3 {
		_, err := svc.Lookup(ctx, "78005100")
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, calls.Load(), "o acerto vem do cache")

	for range 3 {
		_, err := svc.Lookup(ctx, "99999999")
		assert.ErrorIs(t, err, ErrNotFound)
	}
	assert.EqualValues(t, 2, calls.Load(), "o CEP inexistente também é guardado")

	now = now.Add(notFoundTTL + time.Minute)
	_, _ = svc.Lookup(ctx, "99999999")
	assert.EqualValues(t, 3, calls.Load(), "o CEP inexistente expira antes do acerto")
	_, _ = svc.Lookup(ctx, "78005100")
	assert.EqualValues(t, 3, calls.Load())

	// Falhas não são guardadas.
	for range 2 {
		_, _ = svc.Lookup(ctx, "22222222")
	}
	assert.EqualValues(t, 5, calls.Load())
}

func TestHandler(t *testing.T) {
	svc, _ := fakeViaCEP(t)
	pass := func(next http.Handler) http.Handler { return next }
	r := chi.NewRouter()
	NewHandler(svc, pass, pass).Routes(r)

	for path, want := range map[string]struct {
		status int
		code   string
	}{
		"78005-100": {200, ""},
		"123":       {400, "bad_request"},
		"99999999":  {404, "cep_not_found"},
		"22222222":  {502, "cep_unavailable"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/catalog/zip-codes/"+path, nil))
		assert.Equal(t, want.status, w.Code, path)
		if want.code != "" {
			assert.Contains(t, w.Body.String(), `"code":"`+want.code+`"`, path)
		}
	}
}
