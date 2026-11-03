package cep

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// Handler expõe a consulta de CEP em /admin/catalog/zip-codes/{cep}.
type Handler struct {
	svc   *Service
	guard func(http.Handler) http.Handler
	limit func(http.Handler) http.Handler
}

// NewHandler cria o handler. guard autentica o administrador e exige a permissão; limit é o limite de consultas por minuto de
// cada IP, para que a API não vire uma porta para varrer o ViaCEP.
func NewHandler(svc *Service, guard, limit func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, guard: guard, limit: limit}
}

// Routes registra as rotas.
func (h *Handler) Routes(r chi.Router) {
	r.With(h.guard, h.limit).Get("/admin/catalog/zip-codes/{cep}", h.get)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	addr, err := h.svc.Lookup(r.Context(), chi.URLParam(r, "cep"))
	switch {
	case errors.Is(err, ErrInvalid):
		httpapi.Error(w, r, httpapi.ErrBadRequest("O CEP deve ter 8 dígitos."))
	case errors.Is(err, ErrNotFound):
		httpapi.Error(w, r, &httpapi.APIError{Status: http.StatusNotFound, Code: "cep_not_found", Message: "CEP não encontrado."})
	case errors.Is(err, ErrUnavailable):
		httpapi.Error(w, r, &httpapi.APIError{Status: http.StatusBadGateway, Code: "cep_unavailable", Message: "A consulta de CEP está indisponível no momento. Preencha o endereço manualmente."})
	case err != nil:
		httpapi.Error(w, r, err)
	default:
		httpapi.JSON(w, http.StatusOK, addr, nil)
	}
}
