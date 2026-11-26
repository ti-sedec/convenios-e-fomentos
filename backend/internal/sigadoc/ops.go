package sigadoc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var _ Client = (*HTTP)(nil)

const formContentType = "application/x-www-form-urlencoded"

// IsPINRejected diz se o SIGADOC recusou o PIN. A mensagem exata não está documentada; reconhece-se pelo texto. Quem recebe isto
// NUNCA deve tentar de novo sozinho: cada tentativa errada conta para o bloqueio do PIN do analista.
func IsPINRejected(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	m := strings.ToLower(e.Message)
	return strings.Contains(m, "pin") && (strings.Contains(m, "inválid") || strings.Contains(m, "invalid") || strings.Contains(m, "incorret") ||
		strings.Contains(m, "errad") || strings.Contains(m, "bloquead"))
}

// IsNotFound diz se o documento não existe no SIGADOC.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Status == http.StatusNotFound || strings.Contains(e.Message, "não existe"))
}

// check devolve o *Error de uma resposta que não foi 2xx.
func check(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	return parseError(status, body)
}

// formPIN monta o corpo "pin=..." em um buffer que o chamador zera depois do envio, para o PIN não ficar em uma string.
func formPIN(pin PIN) []byte {
	const hex = "0123456789ABCDEF"
	b := append(make([]byte, 0, len(pin)*3+4), "pin="...)
	for _, c := range pin {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '~':
			b = append(b, c)
		default:
			b = append(b, '%', hex[c>>4], hex[c&15])
		}
	}
	return b
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// PreviewTermo gera o HTML do modelo preenchido (POST /sigaex/modelos/{id}/processar-entrevista). Nada é gravado no SIGADOC.
func (c *HTTP) PreviewTermo(ctx context.Context, tok Token, modelID string, fields map[string]string) (string, error) {
	if strings.TrimSpace(modelID) == "" || strings.ContainsAny(modelID, "/?#") {
		return "", errors.New("sigadoc: id do modelo inválido")
	}
	// O SIGADOC de desenvolvimento troca por "�" todo caractere acentuado recebido na entrevista (testado em 24/11/2026 com UTF-8,
	// Latin-1, \u e multipart). Como a resposta é um formulário HTML com os valores em atributos, a pré-visualização manda os
	// não-ASCII como referências numéricas (&#231;), que o navegador decodifica. Isso vale só aqui: como a criação do documento trata
	// acentos ainda precisa ser confirmado (docs/planejamento-integracao-sigadoc.md, G3).
	ascii := make(map[string]string, len(fields))
	for k, v := range fields {
		ascii[k] = asciiEntities(v)
	}
	interview, err := json.Marshal(ascii)
	if err != nil {
		return "", err
	}
	form := url.Values{"entrevista": {string(interview)}}
	status, body, err := c.call(ctx, tok, http.MethodPost, "/sigaex/modelos/"+modelID+"/processar-entrevista", nil, formContentType, []byte(form.Encode()))
	if err != nil {
		return "", err
	}
	if err := check(status, body); err != nil {
		return "", err
	}
	return string(body), nil
}

// asciiEntities troca cada caractere fora do ASCII por sua referência numérica HTML (ç vira &#231;).
func asciiEntities(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
			continue
		}
		fmt.Fprintf(&b, "&#%d;", r)
	}
	return b.String()
}

// ValidatePIN confere o PIN (POST /siga/pin/validar). A documentação do SIGADOC exige o par CPF:PIN em Base64 na QUERY; por isso a
// URL desta chamada nunca é registrada nem devolvida em erro (veja send).
func (c *HTTP) ValidatePIN(ctx context.Context, tok Token, cpf string, pin PIN) error {
	raw := append(append([]byte(cpf), ':'), pin...)
	enc := base64.StdEncoding.EncodeToString(raw)
	zero(raw)
	status, body, err := c.call(ctx, tok, http.MethodPost, "/siga/pin/validar", url.Values{"pinBasicAuth": {enc}}, "", nil)
	if err != nil {
		return err
	}
	return check(status, body)
}

// CreateDocument cria o documento (POST /sigaex/documentos, multipart) e devolve a sigla.
func (c *HTTP) CreateDocument(ctx context.Context, tok Token, in NewDocument) (string, error) {
	if strings.TrimSpace(in.Model) == "" {
		return "", errors.New("sigadoc: modelo do documento é obrigatório")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	field := func(k, v string) error {
		if v == "" {
			return nil
		}
		return w.WriteField(k, v)
	}
	for _, kv := range [][2]string{
		{"modelo", in.Model}, {"descricaotipodoc", in.DocType}, {"descricaodocumento", in.Description},
		{"classificacao", in.Classification}, {"subscritor", in.Subscriber}, {"nivelacesso", in.AccessLevel}, {"lotadestinatario", in.DestinationLotacao},
	} {
		if err := field(kv[0], kv[1]); err != nil {
			return "", err
		}
	}
	if in.Electronic {
		if err := w.WriteField("eletronico", "true"); err != nil {
			return "", err
		}
	}
	// "entrevista" é obrigatório na criação (swagger: required). O documento capturado não tem campos de modelo, e o sistema que já
	// abre processos em produção manda um valor qualquer ("a=a"); sem o campo, o PDF não chega a ser gravado no documento e a assinatura
	// falha por dentro (`input is null`).
	switch {
	case len(in.Interview) > 0:
		j, err := json.Marshal(in.Interview)
		if err != nil {
			return "", err
		}
		if err := w.WriteField("entrevista", string(j)); err != nil {
			return "", err
		}
	case in.File != nil:
		if err := w.WriteField("entrevista", "a=a"); err != nil {
			return "", err
		}
	}
	if in.File != nil {
		name := in.Filename
		if name == "" {
			name = "documento.pdf"
		}
		// O SIGADOC decide pelo tipo do arquivo: sem application/pdf (o padrão do CreateFormFile é octet-stream) o conteúdo pode não ser
		// gravado como PDF do documento capturado.
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="arquivo"; filename="%s"`, escapeQuotes(name)))
		h.Set("Content-Type", "application/pdf")
		fw, err := w.CreatePart(h)
		if err != nil {
			return "", err
		}
		if _, err := fw.Write(in.File); err != nil {
			return "", err
		}
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	status, body, err := c.call(ctx, tok, http.MethodPost, "/sigaex/documentos", nil, w.FormDataContentType(), buf.Bytes())
	if err != nil {
		return "", err
	}
	if err := check(status, body); err != nil {
		return "", err
	}
	var out struct {
		Sigla string `json:"sigladoc"`
	}
	if json.Unmarshal(body, &out) != nil || out.Sigla == "" {
		return "", errors.New("sigadoc: a criação do documento não devolveu a sigla")
	}
	return out.Sigla, nil
}

// plainSigla tira da sigla o que não é letra nem número: o SIGADOC só aceita o número sem traços nem barras nas chamadas de ação
// ("SEDEC-DIC-2026/00014-A" vira "SEDECDIC202600014A"). Informado pela MTI.
func plainSigla(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return strings.ToUpper(b.String())
}

func siglaPath(sigla string) (string, error) {
	plain := plainSigla(sigla)
	if plain == "" {
		return "", fmt.Errorf("sigadoc: sigla inválida %q", sigla)
	}
	return "/sigaex/documentos/" + plain, nil
}

// Finalize finaliza a elaboração (POST /sigaex/documentos/{sigla}/finalizar).
func (c *HTTP) Finalize(ctx context.Context, tok Token, sigla string) (string, error) {
	p, err := siglaPath(sigla)
	if err != nil {
		return "", err
	}
	status, body, err := c.call(ctx, tok, http.MethodPost, p+"/finalizar", nil, formContentType, []byte{})
	if err != nil {
		return "", err
	}
	if err := check(status, body); err != nil {
		return "", err
	}
	var out struct {
		Sigla string `json:"sigla"`
	}
	if json.Unmarshal(body, &out) == nil {
		if def := plainSigla(out.Sigla); def != "" {
			return def, nil
		}
	}
	return plainSigla(sigla), nil
}

// Sign assina com o PIN (POST /sigaex/documentos/{sigla}/assinar-com-pin). O PIN vai no corpo do formulário, e o buffer é zerado
// depois do envio.
func (c *HTTP) Sign(ctx context.Context, tok Token, sigla string, pin PIN) error {
	return c.pinAction(ctx, tok, sigla, "/assinar-com-pin", pin)
}

// AutenticarDocumento autentica o documento capturado com o PIN (POST /sigaex/documentos/{sigla}/autenticar-com-pin).
func (c *HTTP) AutenticarDocumento(ctx context.Context, tok Token, sigla string, pin PIN) error {
	return c.pinAction(ctx, tok, sigla, "/autenticar-com-pin", pin)
}

func (c *HTTP) pinAction(ctx context.Context, tok Token, sigla, action string, pin PIN) error {
	p, err := siglaPath(sigla)
	if err != nil {
		return err
	}
	body := formPIN(pin)
	defer zero(body)
	status, resp, err := c.call(ctx, tok, http.MethodPost, p+action, nil, formContentType, body)
	if err != nil {
		return err
	}
	return check(status, resp)
}

// Autuar autua o documento (POST /sigaex/documentos/{sigla}/autuar) e devolve o id informado.
func (c *HTTP) Autuar(ctx context.Context, tok Token, sigla string) (string, error) {
	p, err := siglaPath(sigla)
	if err != nil {
		return "", err
	}
	status, body, err := c.call(ctx, tok, http.MethodPost, p+"/autuar", nil, formContentType, []byte{})
	if err != nil {
		return "", err
	}
	if err := check(status, body); err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &out)
	return out.ID, nil
}

// Join junta o documento à via ou ao volume do processo (POST /sigaex/documentos/{sigla}/juntar?siglapai=...).
func (c *HTTP) Join(ctx context.Context, tok Token, sigla, siglaPai string) error {
	// A juntada trabalha com a VIA ou o VOLUME, não com o documento: o filho (expediente) leva a letra da via ("A") e o pai (processo) o
	// volume ("V01"). Sem isso o SIGADOC recusa: "Proibido porque não é via".
	filho := withMobil(plainSigla(sigla), "A")
	if _, err := siglaPath(filho); err != nil {
		return err
	}
	pai := withMobil(plainSigla(siglaPai), "V01")
	if pai == "" {
		return fmt.Errorf("sigadoc: sigla do processo inválida %q", siglaPai)
	}
	status, body, err := c.call(ctx, tok, http.MethodPost, "/sigaex/documentos/"+filho+"/juntar", url.Values{"siglapai": {pai}}, formContentType, []byte{})
	if err != nil {
		return err
	}
	return check(status, body)
}

var mobilSuffix = regexp.MustCompile(`(?:[A-Z]|V[0-9]{1,2})$`)

// withMobil acrescenta o sufixo da via ou do volume quando a sigla ainda não o tem. A sigla já traz o sufixo se termina em letra (via) ou
// em "V" e um ou dois dígitos (volume); o corpo do número ("…202600134") só tem dígitos no fim.
func withMobil(sigla, suffix string) string {
	if sigla == "" || mobilSuffix.MatchString(sigla) {
		return sigla
	}
	return sigla + suffix
}

// FindByDescription não é suportado pelo cliente real: a pesquisa (GET /sigaex/documentos) só filtra por marcador, pessoa, lotação e
// período, e o formato da resposta não está documentado. Quem chama trata ErrUnsupported (avisa o analista e segue).
func (c *HTTP) FindByDescription(context.Context, Token, string, time.Time) (string, bool, error) {
	return "", false, ErrUnsupported
}

// Movements consulta as movimentações (GET /sigaex/documentos/{sigla}/movimentacoes).
func (c *HTTP) Movements(ctx context.Context, tok Token, sigla string) (MovementList, error) {
	p, err := siglaPath(sigla)
	if err != nil {
		return MovementList{}, err
	}
	status, body, err := c.call(ctx, tok, http.MethodGet, p+"/movimentacoes", nil, "", nil)
	if err != nil {
		return MovementList{}, err
	}
	if err := check(status, body); err != nil {
		return MovementList{}, err
	}
	return parseMovements(body)
}

// parseMovements lê {"movimentacaoResposta": {...}}.
func parseMovements(body []byte) (MovementList, error) {
	var raw struct {
		R struct {
			Sigla       string `json:"siglaDocumento"`
			Description string `json:"descricaoDocumento"`
			Situation   string `json:"situacaoDocumento"`
			Items       []struct {
				ID          int64  `json:"id"`
				TypeID      int    `json:"idTipoMovimentacao"`
				Lotacao     string `json:"siglaLotacaoCadastrante"`
				Event       string `json:"evento"`
				Destination string `json:"siglaLotacaoAtendente"`
				Person      string `json:"nomePessoaAtendente"`
				Date        string `json:"data"`
			} `json:"movimentacoes"`
		} `json:"movimentacaoResposta"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return MovementList{}, fmt.Errorf("sigadoc: resposta de movimentações inválida: %w", err)
	}
	out := MovementList{Sigla: raw.R.Sigla, Description: raw.R.Description, Situation: raw.R.Situation}
	for _, m := range raw.R.Items {
		out.Items = append(out.Items, Movement{ID: m.ID, TypeID: m.TypeID, Event: m.Event, Lotacao: m.Lotacao, Destination: m.Destination,
			Person: m.Person, At: parseDate(m.Date), RawDate: m.Date})
	}
	return out, nil
}

// parseDate tenta os formatos que o SIGADOC costuma usar (a data de "movimentacoes" não é documentada). Sem sucesso, devolve zero.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	loc, err := time.LoadLocation("America/Cuiaba")
	if err != nil {
		loc = time.FixedZone("-04", -4*3600)
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "02/01/2006 15:04:05", "02/01/06 15:04:05", "02/01/2006 15:04", "02/01/2006", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }
