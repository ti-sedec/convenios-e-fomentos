// Pacote cep consulta o endereço de um CEP no ViaCEP (gratuito e sem chave). O navegador nunca fala com o ViaCEP: a consulta passa
// pela API, que valida o CEP, limita o tempo de espera e guarda o resultado em memória, porque um CEP quase nunca muda e o
// serviço bloqueia quem faz consultas em massa.
package cep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalid indica um CEP que não tem 8 dígitos.
	ErrInvalid = errors.New("CEP inválido")
	// ErrNotFound indica um CEP que o ViaCEP não conhece.
	ErrNotFound = errors.New("CEP não encontrado")
	// ErrUnavailable indica que o ViaCEP não respondeu ou respondeu algo inesperado.
	ErrUnavailable = errors.New("serviço de CEP indisponível")
)

const (
	length       = 8
	maxBody      = 16 << 10
	foundTTL     = 7 * 24 * time.Hour
	notFoundTTL  = time.Hour
	maxCacheSize = 10000
)

// Address é o endereço de um CEP. IBGECode é o código de 7 dígitos do município, o mesmo do catálogo de municípios.
type Address struct {
	Zip      string `json:"zip"`
	Street   string `json:"street"`
	District string `json:"district"`
	City     string `json:"city"`
	UF       string `json:"uf"`
	IBGECode string `json:"ibgeCode"`
}

// Normalize remove a pontuação e confere que sobram 8 dígitos.
func Normalize(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '.' || r == ' ':
		default:
			return "", ErrInvalid
		}
	}
	if b.Len() != length {
		return "", ErrInvalid
	}
	return b.String(), nil
}

type entry struct {
	addr    *Address // nil: o CEP não existe
	expires time.Time
}

// Service consulta o ViaCEP com cache.
type Service struct {
	baseURL string
	http    *http.Client
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]entry
}

// NewService cria o serviço. baseURL é a raiz do ViaCEP (https://viacep.com.br/ws); timeout limita cada consulta.
func NewService(baseURL string, timeout time.Duration) *Service {
	return &Service{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		now:     time.Now,
		cache:   map[string]entry{},
	}
}

// Lookup devolve o endereço do CEP (com ou sem hífen).
func (s *Service) Lookup(ctx context.Context, raw string) (*Address, error) {
	zip, err := Normalize(raw)
	if err != nil {
		return nil, err
	}
	if e, ok := s.cached(zip); ok {
		if e.addr == nil {
			return nil, ErrNotFound
		}
		return e.addr, nil
	}

	addr, err := s.fetch(ctx, zip)
	switch {
	case errors.Is(err, ErrNotFound):
		s.store(zip, nil, notFoundTTL)
	case err == nil:
		s.store(zip, addr, foundTTL)
	}
	return addr, err
}

func (s *Service) cached(zip string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[zip]
	if !ok || !e.expires.After(s.now()) {
		delete(s.cache, zip)
		return entry{}, false
	}
	return e, true
}

func (s *Service) store(zip string, addr *Address, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= maxCacheSize {
		s.cache = map[string]entry{}
	}
	s.cache[zip] = entry{addr: addr, expires: s.now().Add(ttl)}
}

// viaCEP é a resposta do ViaCEP. Um CEP desconhecido vem como {"erro": true} (ou "true", conforme a versão).
type viaCEP struct {
	Erro       any    `json:"erro"`
	Logradouro string `json:"logradouro"`
	Bairro     string `json:"bairro"`
	Localidade string `json:"localidade"`
	UF         string `json:"uf"`
	IBGE       string `json:"ibge"`
}

func (s *Service) fetch(ctx context.Context, zip string) (*Address, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/"+zip+"/json/", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	var v viaCEP
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if v.Erro != nil && v.Erro != false && v.Erro != "false" {
		return nil, ErrNotFound
	}
	return &Address{
		Zip: zip, Street: v.Logradouro, District: v.Bairro, City: v.Localidade,
		UF: strings.ToUpper(v.UF), IBGECode: v.IBGE,
	}, nil
}
