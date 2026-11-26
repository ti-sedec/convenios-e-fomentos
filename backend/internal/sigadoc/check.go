package sigadoc

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result é o resultado da sondagem de um método.
type Result string

const (
	// Allowed: o SIGADOC chamou o método (qualquer resposta que não seja "sem acesso"; um 400 ou 404 de validação também conta,
	// porque a checagem de acesso vem antes da validação).
	Allowed Result = "allowed"
	// Blocked: 403 "O usuário de sistema não possui acesso a este método". A MTI precisa liberar.
	Blocked Result = "blocked"
	// NotDeployed: o método consta no swagger, mas a versão publicada não o tem (ClassNotFoundException).
	NotDeployed Result = "not_deployed"
	// Unknown: sem resposta utilizável (rede, 401...).
	Unknown Result = "unknown"
	// Skipped: não sondado de propósito (veja Probe.Skip).
	Skipped Result = "skipped"
)

// probeSigla é uma sigla que não existe. Chamar métodos de escrita com ela não altera nada: o SIGADOC confere o acesso ao método e
// depois responde "documento não existe".
const probeSigla = "SEDECMEM299999999A"

// Probe descreve um método do SIGADOC que o fluxo usa e como sondá-lo sem efeito.
type Probe struct {
	// Step é a etapa do fluxo que depende do método (para agrupar o relatório).
	Step string
	// Method e Path são o do swagger, já com a sigla de teste no lugar de {sigla}.
	Method, Path string
	Query        url.Values
	// Form, se houver, vai como x-www-form-urlencoded; JSON, se houver, como application/json.
	Form url.Values
	JSON string
	// Required diz se o fluxo da abertura do processo não anda sem o método.
	Required bool
	// Skip, se preenchido, é o motivo de NÃO sondar (o método teria efeito ou gastaria uma tentativa de PIN).
	Skip string
}

// Display é o caminho como aparece no swagger, com {sigla} e {id} no lugar dos valores de teste.
func (p Probe) Display() string {
	return strings.NewReplacer(probeSigla, "{sigla}", "/modelos/1", "/modelos/{id}").Replace(p.Path)
}

// Outcome é o resultado de um Probe.
type Outcome struct {
	Probe  Probe
	Result Result
	Status int
	// Detail é a mensagem curta do SIGADOC, sem stacktrace.
	Detail string
}

// Probes é a lista dos métodos de que a abertura e o acompanhamento do processo dependem (docs/planejamento-integracao-sigadoc.md).
func Probes(now time.Time) []Probe {
	s := probeSigla
	day := now.Format(time.DateOnly)
	prev := now.AddDate(0, 0, -1).Format(time.DateOnly)
	return []Probe{
		{Step: "Matrícula do analista", Method: http.MethodGet, Path: "/siga/matricula", Query: url.Values{"cpf": {"00000000000"}, "sigla": {"SEDEC"}}, Required: true},
		{Step: "Termo de Abertura", Method: http.MethodGet, Path: "/sigaex/modelos", Required: true},
		{Step: "Termo de Abertura", Method: http.MethodGet, Path: "/sigaex/modelos/1", Required: true},
		{Step: "Termo de Abertura", Method: http.MethodPost, Path: "/sigaex/modelos/1/processar-entrevista", Form: url.Values{"entrevista": {""}}, Required: true},
		{Step: "PIN", Method: http.MethodPost, Path: "/siga/pin/validar", Required: true,
			Skip: "gastaria uma tentativa do PIN do dono da matrícula; só dá para conferir com um PIN de teste informado de propósito"},
		{Step: "Criar documento", Method: http.MethodPost, Path: "/sigaex/documentos", Required: true,
			Skip: "criaria um documento de verdade no SIGADOC"},
		{Step: "Finalizar", Method: http.MethodPost, Path: "/sigaex/documentos/" + s + "/finalizar", Required: true},
		{Step: "Assinar com PIN", Method: http.MethodPost, Path: "/sigaex/documentos/" + s + "/assinar-com-pin", Form: url.Values{}, Required: true},
		{Step: "Autuar", Method: http.MethodPost, Path: "/sigaex/documentos/" + s + "/autuar", Required: true},
		{Step: "Juntar ao processo", Method: http.MethodPost, Path: "/sigaex/documentos/" + s + "/juntar", Form: url.Values{"siglapai": {"SEDECMEM299999998V01"}}, Required: true},
		{Step: "Consultar o documento", Method: http.MethodGet, Path: "/sigaex/documentos/" + s, Required: true},
		{Step: "Acompanhar movimentações", Method: http.MethodGet, Path: "/sigaex/documentos/" + s + "/movimentacoes", Required: true},
		{Step: "Pesquisar documentos", Method: http.MethodGet, Path: "/sigaex/documentos", Query: url.Values{"dtinicial": {prev}, "dtfinal": {day}, "ordenacao": {"1"}, "qtdmax": {"1"}}},
		{Step: "Ler o processo", Method: http.MethodGet, Path: "/sigaex/documentos/" + s + "/pdf-completo"},
		{Step: "Ler o processo", Method: http.MethodGet, Path: "/sigaex/documentos/" + s + "/dossie"},
		{Step: "Ler o processo", Method: http.MethodGet, Path: "/sigaex/documentos/" + s + "/consultar-protocolo"},
		{Step: "Reconhecer o arquivamento", Method: http.MethodGet, Path: "/sigaex/documentos/" + s + "/consultar-arquivado"},
		{Step: "Tramitar (fora do escopo atual)", Method: http.MethodPost, Path: "/sigaex/documentos/" + s + "/tramitar", Form: url.Values{"lotacao": {"SEDEC"}}},
	}
}

// Check sonda cada método com o token de uma matrícula e devolve o resultado, na ordem de Probes. Não altera nada no SIGADOC.
func (c *HTTP) Check(ctx context.Context, token Token, now time.Time) []Outcome {
	probes := Probes(now)
	out := make([]Outcome, 0, len(probes))
	for _, p := range probes {
		if p.Skip != "" {
			out = append(out, Outcome{Probe: p, Result: Skipped, Detail: p.Skip})
			continue
		}
		var (
			ct   string
			body []byte
		)
		switch {
		case p.Form != nil:
			ct, body = "application/x-www-form-urlencoded", []byte(p.Form.Encode())
		case p.JSON != "":
			ct, body = "application/json", []byte(p.JSON)
		}
		status, data, err := c.call(ctx, token, p.Method, p.Path, p.Query, ct, body)
		if err != nil {
			out = append(out, Outcome{Probe: p, Result: Unknown, Detail: err.Error()})
			continue
		}
		res, detail := classify(status, data)
		out = append(out, Outcome{Probe: p, Result: res, Status: status, Detail: detail})
	}
	return out
}

// classify interpreta a resposta de uma sondagem.
func classify(status int, body []byte) (Result, string) {
	msg := messageOf(body)
	switch {
	case isMethodForbidden(status, msg):
		return Blocked, "sem acesso ao método"
	case strings.Contains(msg, "ClassNotFoundException") || strings.Contains(msg, "from [Module"):
		return NotDeployed, "método não implantado nesta versão do SIGADOC"
	case status == http.StatusUnauthorized:
		return Unknown, "token recusado (401)"
	case status >= 200 && status < 300:
		return Allowed, "ok"
	default:
		if len(msg) > 90 {
			msg = msg[:90] + "…"
		}
		return Allowed, msg
	}
}
