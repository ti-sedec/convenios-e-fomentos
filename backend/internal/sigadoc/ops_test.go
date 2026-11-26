package sigadoc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPINNuncaApareceNaSaida(t *testing.T) {
	p := NewPIN("24681357")
	assert.NotContains(t, fmt.Sprintf("%s %v %+v %#v %q %x", p, p, p, p, p, p), "7418", "%x imprime os bytes: o PIN só passa pela API explícita")
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "pin", p)
	assert.NotContains(t, buf.String(), "24681357")
	p.Zero()
	assert.Equal(t, PIN{0, 0, 0, 0, 0, 0, 0, 0}, p, "Zero apaga o buffer")
}

func TestFormPIN(t *testing.T) {
	b := formPIN(NewPIN("24681357"))
	assert.Equal(t, "pin=24681357", string(b))
	assert.Equal(t, "pin=a%26b%3Dc%20d", string(formPIN(NewPIN("a&b=c d"))), "escapa o que não é seguro em formulário")
	zero(b)
	assert.Equal(t, make([]byte, len(b)), b)
}

func TestSignEnviaOPinNoCorpoENuncaNaUrl(t *testing.T) {
	var logs bytes.Buffer
	c := newTestClient(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, testService+"/sigaex/documentos/SEDECMEM202600001A/assinar-com-pin", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery, "o PIN não vai na URL")
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		b, _ := io.ReadAll(r.Body)
		assert.Equal(t, "pin=24681357", string(b))
		_, _ = w.Write([]byte(`{"sigla":"SEDECMEM202600001A","status":"OK"}`))
	})
	require.NoError(t, c.Sign(t.Context(), Token{Value: "tok"}, "SEDECMEM202600001A", NewPIN("24681357")))
	assert.NotContains(t, logs.String(), "24681357")
}

// O capturado (PDF que subiu) se autentica; só o documento produzido no SIGADOC se assina.
func TestAutenticarDocumentoUsaARotaDeAutenticacaoComOPinNoCorpo(t *testing.T) {
	var logs bytes.Buffer
	c := newTestClient(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, testService+"/sigaex/documentos/SEDECMEM202600002A/autenticar-com-pin", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery, "o PIN não vai na URL")
		b, _ := io.ReadAll(r.Body)
		assert.Equal(t, "pin=24681357", string(b))
		_, _ = w.Write([]byte(`{"sigla":"SEDECMEM202600002A","status":"OK"}`))
	})
	require.NoError(t, c.AutenticarDocumento(t.Context(), Token{Value: "tok"}, "SEDECMEM202600002A", NewPIN("24681357")))
	assert.NotContains(t, logs.String(), "24681357")
}

// A validação do PIN leva o par CPF:PIN em Base64 na query. Se a rede falhar, o erro do net/http traria a URL: não pode sair.
func TestValidatePINNaoVazaNoErroDeRede(t *testing.T) {
	var logs bytes.Buffer
	var gotQuery string
	srv := newTestClient(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("pinBasicAuth")
		_, _ = w.Write([]byte(`{}`))
	})
	require.NoError(t, srv.ValidatePIN(t.Context(), Token{Value: "tok"}, "52998224725", NewPIN("24681357")))
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("52998224725:24681357")), gotQuery)

	// Servidor que não existe: o erro de conexão não pode conter a query.
	dead := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead.Close()
	c, err := NewHTTP(HTTPConfig{BaseURL: dead.URL + testService, XRoadClient: testXRoad, Username: testUser, Password: testPass, Log: slog.New(slog.NewTextHandler(&logs, nil))})
	require.NoError(t, err)
	err = c.ValidatePIN(t.Context(), Token{Value: "tok"}, "52998224725", NewPIN("24681357"))
	require.Error(t, err)
	enc := base64.StdEncoding.EncodeToString([]byte("52998224725:24681357"))
	assert.NotContains(t, err.Error(), enc)
	assert.NotContains(t, err.Error(), "pinBasicAuth")
	assert.NotContains(t, logs.String(), enc)
}

func TestCriarDocumentoCapturado(t *testing.T) {
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testService+"/sigaex/documentos", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "Relatório do projeto", r.FormValue("modelo"))
		assert.Equal(t, "Interno Capturado", r.FormValue("descricaotipodoc"))
		assert.Equal(t, "Relatório — Projeto 2026-000001", r.FormValue("descricaodocumento"))
		assert.Equal(t, "true", r.FormValue("eletronico"))
		assert.Equal(t, "MT123", r.FormValue("subscritor"))
		assert.Equal(t, "a=a", r.FormValue("entrevista"), "o capturado manda a entrevista (obrigatória) com um valor qualquer")
		f, h, err := r.FormFile("arquivo")
		require.NoError(t, err)
		b, _ := io.ReadAll(f)
		assert.Equal(t, "%PDF-1.7 teste", string(b))
		assert.Equal(t, "relatorio.pdf", h.Filename)
		assert.Equal(t, "application/pdf", h.Header.Get("Content-Type"))
		_ = json.NewEncoder(w).Encode(map[string]string{"sigladoc": "SEDECMEM202600007A"})
	})
	sigla, err := c.CreateDocument(t.Context(), Token{Value: "tok"}, NewDocument{Model: "Relatório do projeto", DocType: "Interno Capturado",
		Description: "Relatório — Projeto 2026-000001", Subscriber: "MT123", Electronic: true, File: []byte("%PDF-1.7 teste"), Filename: "relatorio.pdf"})
	require.NoError(t, err)
	assert.Equal(t, "SEDECMEM202600007A", sigla)

	_, err = c.CreateDocument(t.Context(), Token{Value: "tok"}, NewDocument{})
	assert.Error(t, err, "modelo é obrigatório")
}

func TestOperacoesDeDocumento(t *testing.T) {
	var seen []string
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+strings.TrimPrefix(r.URL.Path, testService)+"?"+r.URL.RawQuery)
		switch {
		case strings.HasSuffix(r.URL.Path, "/autuar"):
			_, _ = w.Write([]byte(`{"id":"12345"}`))
		case strings.HasSuffix(r.URL.Path, "/finalizar"):
			// A finalização devolve a sigla definitiva, com traços e barra: o número provisório deixa de valer.
			_, _ = w.Write([]byte(`{"sigla":"SEDEC-MEM-2026/00001-A","status":"OK"}`))
		default:
			_, _ = w.Write([]byte(`{"status":"OK"}`))
		}
	})
	tok, s := Token{Value: "tok"}, "SEDECMEM202600001A"
	def, err := c.Finalize(t.Context(), tok, s)
	require.NoError(t, err)
	assert.Equal(t, "SEDECMEM202600001A", def, "a sigla definitiva volta sem caracteres especiais")
	id, err := c.Autuar(t.Context(), tok, s)
	require.NoError(t, err)
	assert.Equal(t, "12345", id)
	require.NoError(t, c.Join(t.Context(), tok, "SEDECMEM202600002A", "SEDECMEM202600001V01"))
	assert.Equal(t, []string{
		"POST /sigaex/documentos/SEDECMEM202600001A/finalizar?",
		"POST /sigaex/documentos/SEDECMEM202600001A/autuar?",
		"POST /sigaex/documentos/SEDECMEM202600002A/juntar?siglapai=SEDECMEM202600001V01",
	}, seen)
	for _, bad := range []string{"", "-/ ", "?#"} {
		_, err := c.Finalize(t.Context(), tok, bad)
		assert.Error(t, err, "sigla %q", bad)
	}
	assert.Error(t, c.Join(t.Context(), tok, s, ""))

	// O cliente real manda a via no filho e o volume no pai quando a sigla vem só com o número.
	seen = nil
	require.NoError(t, c.Join(t.Context(), tok, "SEDEC-DIC-2026/00016", "SEDEC-PRO-2026/00134"))
	assert.Equal(t, []string{"POST /sigaex/documentos/SEDECDIC202600016A/juntar?siglapai=SEDECPRO202600134V01"}, seen)

	// O número provisório da criação ("TMP-…") segue sem traços nas chamadas (regra da MTI).
	seen = nil
	_, err = c.Finalize(t.Context(), tok, "TMP-20985654")
	require.NoError(t, err)
	assert.Equal(t, []string{"POST /sigaex/documentos/TMP20985654/finalizar?"}, seen)
}

func TestMovements(t *testing.T) {
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testService+"/sigaex/documentos/SEDECPRO202600001V01/movimentacoes", r.URL.Path)
		_, _ = w.Write([]byte(`{"movimentacaoResposta":{"siglaDocumento":"SEDEC-PRO-2026/00001-V01","descricaoDocumento":"Projeto 2026-000001","situacaoDocumento":"Aguardando andamento",
			"movimentacoes":[{"id":11,"idTipoMovimentacao":5,"siglaLotacaoCadastrante":"SEDECCONV","evento":"Juntada","siglaLotacaoAtendente":"SEDECCONV","nomePessoaAtendente":"Maria","data":"17/11/2026 09:30:00"},
			{"id":12,"idTipoMovimentacao":9,"siglaLotacaoCadastrante":"SEDECCONV","evento":"Arquivamento corrente","siglaLotacaoAtendente":"SEDECCONV","nomePessoaAtendente":"Maria","data":"data estranha"}]},"dataConsulta":"x"}`))
	})
	l, err := c.Movements(t.Context(), Token{Value: "tok"}, "SEDECPRO202600001V01")
	require.NoError(t, err)
	assert.Equal(t, "Projeto 2026-000001", l.Description)
	require.Len(t, l.Items, 2)
	assert.EqualValues(t, 11, l.Items[0].ID)
	assert.Equal(t, time.Date(2026, 11, 17, 9, 30, 0, 0, l.Items[0].At.Location()), l.Items[0].At)
	assert.True(t, l.Items[1].At.IsZero(), "data que não se reconhece fica vazia")
	assert.Equal(t, "data estranha", l.Items[1].RawDate)
	assert.True(t, l.IsArchived(), "a última movimentação é um arquivamento")
}

func TestIsArchived(t *testing.T) {
	mv := func(id int64, e string) Movement { return Movement{ID: id, Event: e} }
	assert.False(t, MovementList{}.IsArchived())
	assert.True(t, MovementList{Situation: "Arquivado"}.IsArchived())
	assert.False(t, MovementList{Situation: "Aguardando andamento", Items: []Movement{mv(1, "Juntada")}}.IsArchived())
	assert.False(t, MovementList{Items: []Movement{mv(3, "Desarquivamento"), mv(2, "Arquivamento corrente")}}.IsArchived(), "desarquivado depois (mesmo fora de ordem)")
	assert.True(t, MovementList{Items: []Movement{mv(2, "Desarquivamento"), mv(3, "Arquivamento corrente")}}.IsArchived())
	assert.False(t, MovementList{Items: []Movement{mv(1, "Juntada de Certidão de Arquivamento de Ata")}}.IsArchived(), "só o evento que começa com arquivamento vale")
	assert.False(t, MovementList{Situation: "Desarquivado"}.IsArchived())
}

func TestErroDePinRecusado(t *testing.T) {
	assert.True(t, IsPINRejected(&Error{Status: 500, Message: "PIN inválido"}))
	assert.True(t, IsPINRejected(&Error{Status: 500, Message: "O pin informado está incorreto"}))
	assert.False(t, IsPINRejected(&Error{Status: 500, Message: "Documento não finalizado"}))
	assert.False(t, IsPINRejected(fmt.Errorf("rede")))
	assert.True(t, IsNotFound(&Error{Status: 404, Message: "x"}))
	assert.True(t, IsNotFound(&Error{Status: 500, Message: "Número do Documento não existe"}))
}

func TestSessionGuardaOTokenAteVencer(t *testing.T) {
	m := NewMemory()
	s := NewSession(m)
	a, err := s.Token(t.Context(), " sedec123456 ")
	require.NoError(t, err)
	b, err := s.Token(t.Context(), "SEDEC123456")
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Equal(t, 1, countCalls(m, "Authenticate"), "autentica uma vez só")
	s.Forget("SEDEC123456")
	_, err = s.Token(t.Context(), "SEDEC123456")
	require.NoError(t, err)
	assert.Equal(t, 2, countCalls(m, "Authenticate"))

	// Token perto de vencer é renovado.
	m.SetClock(func() time.Time { return time.Now().Add(-7*time.Hour - 58*time.Minute) })
	s2 := NewSession(m)
	_, err = s2.Token(t.Context(), "SEDEC654321")
	require.NoError(t, err)
	_, err = s2.Token(t.Context(), "SEDEC654321")
	require.NoError(t, err)
	assert.Equal(t, 4, countCalls(m, "Authenticate"), "faltando menos de 5 minutos, renova")
}

func countCalls(m *Memory, op string) int {
	n := 0
	for _, c := range m.Calls() {
		if c == op {
			n++
		}
	}
	return n
}

// O simulador segue as regras que o SIGADOC real impõe.
func TestSimuladorSegueARegrasDoSigadoc(t *testing.T) {
	ctx := t.Context()
	m := NewMemory()
	tok, err := m.Authenticate(ctx, "SEDEC123456")
	require.NoError(t, err)
	pin := NewPIN("24681357")

	_, err = m.Authenticate(ctx, "123456")
	assert.True(t, IsUnknownIdentity(err), "matrícula sem o órgão não existe")

	termo, err := m.CreateDocument(ctx, tok, NewDocument{Model: "Termo", Description: "Termo de Abertura"})
	require.NoError(t, err)
	assert.Regexp(t, `^TMPMEM\d{5}$`, termo, "a criação devolve o número provisório")

	assert.Error(t, m.Sign(ctx, tok, termo, pin), "assinar antes de finalizar")
	assert.Error(t, m.AutenticarDocumento(ctx, tok, termo, pin), "o documento produzido no SIGADOC não se autentica")
	prov := termo
	termo, err = m.Finalize(ctx, tok, termo)
	require.NoError(t, err)
	assert.Regexp(t, `^SEDECMEM\d{4}\d{5}A$`, termo, "a finalização devolve a sigla definitiva")
	_, err = m.Finalize(ctx, tok, prov)
	assert.True(t, IsNotFound(err), "o número provisório deixa de existir depois de finalizar")
	_, err = m.Finalize(ctx, tok, termo)
	assert.Error(t, err, "finalizar duas vezes")
	_, err = m.Autuar(ctx, tok, termo)
	assert.Error(t, err, "autuar sem assinar")
	require.NoError(t, m.Sign(ctx, tok, termo, pin))
	_, err = m.Autuar(ctx, tok, termo)
	require.NoError(t, err)

	doc, err := m.CreateDocument(ctx, tok, NewDocument{Model: "Externo", DocType: "Externo Capturado", Description: "Estatuto", File: []byte("x")})
	require.NoError(t, err)
	assert.Error(t, m.Join(ctx, tok, doc, termo), "juntar sem assinar")
	doc, err = m.Finalize(ctx, tok, doc)
	require.NoError(t, err)
	assert.Error(t, m.Sign(ctx, tok, doc, pin), "o Externo Capturado não se assina: se autentica")
	require.NoError(t, m.AutenticarDocumento(ctx, tok, doc, pin))
	require.NoError(t, m.Join(ctx, tok, doc, termo))
	assert.Equal(t, termo, m.Parent(doc))
	assert.Error(t, m.Join(ctx, tok, doc, termo), "já juntado")

	l, err := m.Movements(ctx, tok, termo)
	require.NoError(t, err)
	assert.False(t, l.IsArchived())
	assert.Equal(t, "Estatuto", strings.TrimPrefix(l.Items[len(l.Items)-1].Event, "Juntada de "))

	m.Archive(termo, time.Now())
	l, _ = m.Movements(ctx, tok, termo)
	assert.True(t, l.IsArchived())
	d2, _ := m.CreateDocument(ctx, tok, NewDocument{Model: "x"})
	d2, _ = m.Finalize(ctx, tok, d2)
	_ = m.Sign(ctx, tok, d2, pin)

	// O Interno Capturado (o relatório) só se assina; autenticar é do Externo Capturado.
	rel, _ := m.CreateDocument(ctx, tok, NewDocument{Model: "Interno", DocType: "Interno Capturado", Description: "Relatório", File: []byte("x")})
	rel, _ = m.Finalize(ctx, tok, rel)
	assert.Error(t, m.AutenticarDocumento(ctx, tok, rel, pin), "o Interno Capturado não se autentica")
	assert.NoError(t, m.Sign(ctx, tok, rel, pin))
	assert.Error(t, m.Join(ctx, tok, d2, termo), "não se junta a processo arquivado")

	_, err = m.Movements(ctx, tok, "SEDECMEM299999999A")
	assert.True(t, IsNotFound(err))
	_, err = m.Movements(ctx, Token{Value: "lixo"}, termo)
	require.Error(t, err)

	m.SetPIN("52998224725", "11112222")
	assert.True(t, IsPINRejected(m.ValidatePIN(ctx, tok, "52998224725", NewPIN("24681357"))))
	assert.NoError(t, m.ValidatePIN(ctx, tok, "52998224725", NewPIN("11112222")))

	m.FailNext("Finalize", &Error{Status: 500, Message: "SIGADOC fora do ar"})
	_, err = m.Finalize(ctx, tok, d2)
	assert.Error(t, err, "falha programada")
	assert.NotContains(t, strings.Join(m.Calls(), " "), "24681357", "o simulador não registra argumentos")
}

func TestIsGatewayError(t *testing.T) {
	body := []byte(`{"type":"Server.ClientProxy.OutdatedGlobalConf","message":"Global configuration is expired","detail":"603730b0"}`)
	assert.True(t, IsGatewayError(check(500, body)))
	assert.False(t, IsGatewayError(check(500, []byte(`{"errormsg":"Falha de autenticação"}`))))
	assert.False(t, IsGatewayError(errors.New("x")))
}

func TestIsProhibited(t *testing.T) {
	msg := `{"errormsg":"O documento X não pode ser autenticado com senha por A/B - Proibido porque não pode movimentar autenticação de documento com senha por configuração"}`
	assert.True(t, IsProhibited(check(500, []byte(msg))))
	assert.False(t, IsProhibited(check(500, []byte(`{"errormsg":"Falha de autenticação"}`))))
}

func TestIsProhibitedSoParaConfiguracao(t *testing.T) {
	assert.False(t, IsProhibited(check(500, []byte(`{"errormsg":"Não é possível fazer juntada - Proibido porque não é via"}`))), "erro do pedido, não permissão")
}

// A resposta de erro da validação do PIN nunca vai para o log: a requisição leva o par CPF:PIN na query.
func TestErroDeRotaDePinNaoVaiParaOLog(t *testing.T) {
	var logs bytes.Buffer
	c := newTestClient(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errormsg":"falha","echo":"` + r.URL.RawQuery + `"}`))
	})
	_ = c.ValidatePIN(t.Context(), Token{Value: "tok"}, "52998224725", NewPIN("24681357"))
	assert.NotContains(t, logs.String(), "error_body")
	assert.NotContains(t, logs.String(), "pinBasicAuth")

	// Já as outras rotas registram o começo do erro para diagnóstico.
	logs.Reset()
	c2 := newTestClient(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errormsg":"Erro ao assinar"}`))
	})
	_ = c2.Sign(t.Context(), Token{Value: "tok"}, "SEDECMEM202600001A", NewPIN("24681357"))
	assert.Contains(t, logs.String(), "error_body")
	assert.NotContains(t, logs.String(), "24681357")
}
