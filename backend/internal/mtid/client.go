package mtid

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrNoToken indica que a chamada não trouxe o token do MT Login (veja WithToken): o MT-ID não aceita pedido sem ele.
var ErrNoToken = errors.New("mtid: chamada sem o token do MT Login")

// maxBody limita o que se lê de uma resposta do MT-ID (o PDF assinado cabe com folga).
const maxBody = 32 << 20

// HTTPConfig configura o cliente real do MT-ID (SmartPass pelo X-Via).
type HTTPConfig struct {
	// BaseURL é a raiz do X-Via com a instância, ex.: https://host/r1/mt-dev. O serviço (Service) é acrescentado a ela.
	BaseURL string
	// Service é o caminho do serviço no X-Via; padrão GOV/MTI/SMARTPASS.
	Service string
	// XRoadClient vai no cabeçalho x-road-client (identifica o nosso subsistema no X-Via).
	XRoadClient string
	// CAFile é um PEM com o certificado a confiar além dos do sistema (o X-Via de desenvolvimento usa certificado próprio). A
	// verificação do certificado continua ligada.
	CAFile string
	// TLSServerName, se definido, é o nome conferido no certificado do servidor (e enviado no SNI) no lugar do host da URL. Serve
	// ao X-Via de desenvolvimento, cujo certificado não lista o nome DNS pelo qual ele é acessado.
	TLSServerName string
	Timeout       time.Duration
	Log           *slog.Logger
}

// HTTP é o cliente real do MT-ID.
type HTTP struct {
	base   string
	xroad  string
	client *http.Client
	log    *slog.Logger
}

var _ Client = (*HTTP)(nil)

// NewHTTP cria o cliente real. Não faz chamada de rede.
func NewHTTP(cfg HTTPConfig) (*HTTP, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("mtid: URL do X-Via inválida: %q", cfg.BaseURL)
	}
	if cfg.XRoadClient == "" {
		return nil, errors.New("mtid: x-road-client é obrigatório")
	}
	service := strings.Trim(cfg.Service, "/")
	if service == "" {
		service = "GOV/MTI/SMARTPASS"
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.TLSServerName}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("mtid: lendo o certificado do X-Via: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("mtid: %s não contém certificado PEM", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &HTTP{
		base:   strings.TrimRight(cfg.BaseURL, "/") + "/" + service,
		xroad:  cfg.XRoadClient,
		client: &http.Client{Transport: tr, Timeout: timeout},
		log:    log,
	}, nil
}

// do faz a chamada autenticada e devolve o corpo. Status fora de 2xx vira erro (404 vira ErrNotFound).
func (c *HTTP) do(ctx context.Context, op, method, path, contentType string, body io.Reader) ([]byte, error) {
	token, ok := TokenFrom(ctx)
	if !ok {
		return nil, ErrNoToken
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-road-client", c.xroad)
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	start := time.Now()
	res, err := c.client.Do(req)
	if err != nil {
		c.log.WarnContext(ctx, "MT-ID: falha de rede", "op", op, "error", err)
		return nil, fmt.Errorf("mtid %s: %w", op, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("mtid %s: lendo a resposta: %w", op, err)
	}
	// Registro de diagnóstico da integração (primeiros testes contra o serviço real): resposta de texto aparece, arquivo não.
	c.log.DebugContext(ctx, "MT-ID", "op", op, "status", res.StatusCode, "ms", time.Since(start).Milliseconds(),
		"contentType", res.Header.Get("Content-Type"), "bytes", len(data), "body", preview(res.Header.Get("Content-Type"), data))
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("mtid %s: HTTP %d: %s", op, res.StatusCode, preview("text/plain", data))
	}
	return data, nil
}

func preview(contentType string, data []byte) string {
	if !strings.Contains(contentType, "json") && !strings.HasPrefix(contentType, "text/") {
		return "(arquivo)"
	}
	const max = 1500
	if len(data) > max {
		return string(data[:max]) + "…"
	}
	return string(data)
}

func (c *HTTP) Upload(ctx context.Context, filename string, pdf []byte) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(pdf); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	data, err := c.do(ctx, "upload", http.MethodPost, "/file-upload", w.FormDataContentType(), &buf)
	if err != nil {
		return "", err
	}
	var out struct {
		FileID string `json:"fileId"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.FileID == "" {
		return "", fmt.Errorf("mtid upload: resposta sem fileId")
	}
	return out.FileID, nil
}

func (c *HTTP) RequestSignature(ctx context.Context, r Request) (string, error) {
	type coords struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	body := struct {
		UserID           string            `json:"userId"`
		Description      string            `json:"description"`
		FileID           string            `json:"fileId"`
		Detached         bool              `json:"detached"`
		Coordinates      coords            `json:"coordinates"`
		SignatureOnPages []int             `json:"signatureOnPages,omitempty"`
		Metadata         map[string]string `json:"metadata,omitempty"`
	}{r.SignerCPF, r.Description, r.FileID, r.Detached, coords{r.X, r.Y}, r.Pages, r.Metadata}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	data, err := c.do(ctx, "signature-request", http.MethodPost, "/signature-request", "application/json", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	var out struct {
		OrderID string `json:"orderId"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.OrderID == "" {
		return "", fmt.Errorf("mtid signature-request: resposta sem orderId")
	}
	return out.OrderID, nil
}

func (c *HTTP) Status(ctx context.Context, orderID string) (Status, error) {
	data, err := c.do(ctx, "status", http.MethodGet, "/signature/"+url.PathEscape(orderID), "", nil)
	if err != nil {
		return Status{}, err
	}
	var out struct {
		Status   string `json:"status"`
		SignedIn int64  `json:"signedIn"`
		FileID   string `json:"fileId"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Status{}, fmt.Errorf("mtid status: resposta inválida: %w", err)
	}
	st := Status{FileID: out.FileID}
	if out.SignedIn > 0 {
		st.SignedAt = time.UnixMilli(out.SignedIn)
	}
	switch strings.ToUpper(out.Status) {
	case "APPROVED":
		st.State = StateApproved
	case "REJECTED", "DENIED", "REFUSED":
		st.State = StateRejected
	case "EXPIRED":
		st.State = StateExpired
	case "PENDING":
		st.State = StatePending
	default:
		// A documentação só mostra PENDING e APPROVED. Qualquer outro valor segue como pendente, mas fica no log para mapearmos.
		c.log.WarnContext(ctx, "MT-ID: situação desconhecida do pedido, tratada como pendente", "orderId", orderID, "status", out.Status)
		st.State = StatePending
	}
	return st, nil
}

func (c *HTTP) Cancel(ctx context.Context, orderID string) error {
	_, err := c.do(ctx, "cancel", http.MethodDelete, "/signature/"+url.PathEscape(orderID), "", nil)
	return err
}

func (c *HTTP) Download(ctx context.Context, fileID string) ([]byte, error) {
	return c.do(ctx, "download", http.MethodGet, "/file-download/"+url.PathEscape(fileID), "", nil)
}
