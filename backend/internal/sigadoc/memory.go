package sigadoc

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var _ Client = (*Memory)(nil)

var matriculaRe = regexp.MustCompile(`^[A-Z]{2,}[0-9]+$`)

// Memory é um SIGADOC simulado, em memória: guarda documentos, processos e movimentações e segue as regras que o SIGADOC real impõe
// (finalizar antes de assinar, assinar antes de autuar ou juntar, juntar só a um processo). Serve ao desenvolvimento
// (SIGADOC_SIMULATE) e aos testes. NÃO abre processo de verdade e é recusado em produção.
type Memory struct {
	mu   sync.Mutex
	now  func() time.Time
	seq  int
	docs map[string]*memDoc
	// pins guarda o PIN esperado por CPF; um CPF sem registro aceita qualquer PIN com 4 ou mais caracteres.
	pins   map[string]string
	cpfs   map[string][]string
	fail   map[string][]error
	called []string
	// nextMovID numera as movimentações de todo o simulador.
	nextMovID int64
	// hooks rodam ao entrar numa operação, fora da trava (os testes provocam acontecimentos no meio de uma montagem).
	hooks map[string]func()
}

type memDoc struct {
	sigla, description, docType string
	seq                         int
	process, finalized, signed  bool
	archived                    bool
	size                        int
	parent                      string
	movs                        []Movement
}

// NewMemory cria o simulador vazio.
func NewMemory() *Memory {
	return &Memory{now: time.Now, docs: map[string]*memDoc{}, pins: map[string]string{}, cpfs: map[string][]string{}, fail: map[string][]error{}}
}

// SetClock troca o relógio (os testes controlam as datas das movimentações).
func (m *Memory) SetClock(now func() time.Time) { m.mu.Lock(); m.now = now; m.mu.Unlock() }

// SetPIN define o PIN esperado para o CPF.
func (m *Memory) SetPIN(cpf, pin string) { m.mu.Lock(); m.pins[cpf] = pin; m.mu.Unlock() }

// SetMatriculas define as matrículas do CPF (resposta de MatriculaByCPF).
func (m *Memory) SetMatriculas(cpf string, matriculas ...string) {
	m.mu.Lock()
	m.cpfs[cpf] = matriculas
	m.mu.Unlock()
}

// FailNext faz a próxima chamada da operação (o nome do método do Client, ex.: "Sign") devolver err. Várias chamadas enfileiram.
func (m *Memory) FailNext(op string, err error) {
	m.mu.Lock()
	m.fail[op] = append(m.fail[op], err)
	m.mu.Unlock()
}

// Calls devolve, em ordem, as operações chamadas (sem argumentos: nunca há PIN aqui).
func (m *Memory) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.called...)
}

// AddMovement acrescenta uma movimentação a um documento, como se alguém a tivesse feito direto no SIGADOC.
func (m *Memory) AddMovement(sigla, event, lotacao, destination, person string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[sigla]; d != nil {
		m.addMov(d, event, lotacao, destination, person, at)
	}
}

// Archive arquiva o documento no SIGADOC.
func (m *Memory) Archive(sigla string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[sigla]; d != nil {
		d.archived = true
		m.addMov(d, "Arquivamento corrente", "SEDEC", "SEDEC", "Sistema", at)
	}
}

// Documents devolve as siglas existentes, em ordem.
func (m *Memory) Documents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for s := range m.docs {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Description devolve a descrição e o tipo do documento ("" se não existe).
func (m *Memory) Description(sigla string) (description, docType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[sigla]; d != nil {
		return d.description, d.docType
	}
	return "", ""
}

// Parent devolve o processo ao qual o documento foi juntado.
func (m *Memory) Parent(sigla string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[sigla]; d != nil {
		return d.parent
	}
	return ""
}

func (m *Memory) addMov(d *memDoc, event, lotacao, destination, person string, at time.Time) {
	m.nextMovID++
	d.movs = append(d.movs, Movement{ID: m.nextMovID, TypeID: 1, Event: event, Lotacao: lotacao, Destination: destination, Person: person, At: at, RawDate: at.Format("02/01/2006 15:04:05")})
}

// SetHook faz fn rodar (uma vez) na próxima chamada da operação, antes de ela agir. fn não pode chamar o próprio simulador.
func (m *Memory) SetHook(op string, fn func()) {
	m.mu.Lock()
	if m.hooks == nil {
		m.hooks = map[string]func(){}
	}
	m.hooks[op] = fn
	m.mu.Unlock()
}

// enter registra a chamada e devolve a falha programada, se houver. Já com a trava tomada.
func (m *Memory) enter(op string) error {
	m.called = append(m.called, op)
	if fn := m.hooks[op]; fn != nil {
		delete(m.hooks, op)
		m.mu.Unlock()
		fn()
		m.mu.Lock()
	}
	if q := m.fail[op]; len(q) > 0 {
		m.fail[op] = q[1:]
		return q[0]
	}
	return nil
}

func notFound() error {
	return &Error{Status: http.StatusNotFound, Message: "Número do Documento não existe"}
}

func (m *Memory) auth(tok Token) (string, error) {
	mat, ok := strings.CutPrefix(tok.Value, "sim:")
	if !ok || mat == "" {
		return "", &Error{Status: http.StatusUnauthorized, Message: "token inválido"}
	}
	return mat, nil
}

// Authenticate aceita matrículas no formato SEDEC123456 e devolve um token de 8 horas.
func (m *Memory) Authenticate(_ context.Context, matricula string) (Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("Authenticate"); err != nil {
		return Token{}, err
	}
	matricula = strings.TrimSpace(matricula)
	if !matriculaRe.MatchString(matricula) {
		return Token{}, &Error{Status: 500, Message: fmt.Sprintf("Erro no login: Ocorreu um erro tentando localizar a identidade do usuario '%s'.", matricula)}
	}
	return Token{Value: "sim:" + matricula, ExpiresAt: m.now().Add(8 * time.Hour)}, nil
}

// MatriculaByCPF devolve o que foi definido com SetMatriculas.
func (m *Memory) MatriculaByCPF(_ context.Context, tok Token, cpf, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("MatriculaByCPF"); err != nil {
		return nil, err
	}
	if _, err := m.auth(tok); err != nil {
		return nil, err
	}
	return append([]string(nil), m.cpfs[cpf]...), nil
}

// PreviewTermo devolve um HTML simples com os campos, em ordem.
func (m *Memory) PreviewTermo(_ context.Context, tok Token, modelID string, fields map[string]string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("PreviewTermo"); err != nil {
		return "", err
	}
	if _, err := m.auth(tok); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "<html><body><h1>Termo de Abertura (modelo %s)</h1><dl>", modelID)
	for _, k := range keys {
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>", k, fields[k])
	}
	b.WriteString("</dl></body></html>")
	return b.String(), nil
}

func (m *Memory) pinOK(cpf string, pin PIN) bool {
	if want, ok := m.pins[cpf]; ok {
		return string(pin) == want
	}
	return len(pin) >= 4
}

// ValidatePIN confere o PIN esperado para o CPF.
func (m *Memory) ValidatePIN(_ context.Context, tok Token, cpf string, pin PIN) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("ValidatePIN"); err != nil {
		return err
	}
	if _, err := m.auth(tok); err != nil {
		return err
	}
	if !m.pinOK(cpf, pin) {
		return &Error{Status: 500, Message: "PIN inválido"}
	}
	return nil
}

// CreateDocument cria um documento em elaboração.
func (m *Memory) CreateDocument(_ context.Context, tok Token, in NewDocument) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("CreateDocument"); err != nil {
		return "", err
	}
	mat, err := m.auth(tok)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Model) == "" {
		return "", &Error{Status: 500, Message: "Modelo é obrigatório"}
	}
	m.seq++
	// Como o SIGADOC real: a criação devolve um número provisório, e o definitivo só vem na finalização.
	sigla := fmt.Sprintf("TMPMEM%05d", m.seq)
	d := &memDoc{sigla: sigla, seq: m.seq, description: in.Description, docType: in.DocType, size: len(in.File)}
	m.docs[sigla] = d
	m.addMov(d, "Criação do documento", "SEDEC", "SEDEC", mat, m.now())
	return sigla, nil
}

// Finalize finaliza o documento e devolve a sigla definitiva. O número provisório deixa de existir.
func (m *Memory) Finalize(_ context.Context, tok Token, sigla string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("Finalize"); err != nil {
		return "", err
	}
	mat, err := m.auth(tok)
	if err != nil {
		return "", err
	}
	d := m.docs[sigla]
	if d == nil {
		return "", notFound()
	}
	if d.finalized {
		return "", &Error{Status: 500, Message: "Documento já está finalizado"}
	}
	d.finalized = true
	delete(m.docs, sigla)
	d.sigla = fmt.Sprintf("SEDECMEM%d%05dA", m.now().Year(), d.seq)
	m.docs[d.sigla] = d
	m.addMov(d, "Finalização", "SEDEC", "SEDEC", mat, m.now())
	return d.sigla, nil
}

// Sign assina o documento finalizado com o PIN. Como o SIGADOC real, não aceita o documento capturado (que se autentica).
func (m *Memory) Sign(_ context.Context, tok Token, sigla string, pin PIN) error {
	return m.pinStep("Sign", tok, sigla, pin, false)
}

// AutenticarDocumento autentica o documento capturado com o PIN. Só serve ao capturado.
func (m *Memory) AutenticarDocumento(_ context.Context, tok Token, sigla string, pin PIN) error {
	return m.pinStep("AutenticarDocumento", tok, sigla, pin, true)
}

func (m *Memory) pinStep(op string, tok Token, sigla string, pin PIN, captured bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter(op); err != nil {
		return err
	}
	mat, err := m.auth(tok)
	if err != nil {
		return err
	}
	d := m.docs[sigla]
	if d == nil {
		return notFound()
	}
	if !d.finalized {
		return &Error{Status: 500, Message: "Só se assina um documento finalizado"}
	}
	if d.signed {
		return &Error{Status: 500, Message: "Documento já está assinado"}
	}
	if len(pin) < 4 {
		return &Error{Status: 500, Message: "PIN inválido"}
	}
	// Regra informada pela MTI: o Interno Capturado só se assina; o que se autentica é o Externo Capturado.
	if external := strings.Contains(d.docType, "Externo Capturado"); external != captured {
		if captured {
			return &Error{Status: 500, Message: "Só se autentica documento Externo Capturado"}
		}
		return &Error{Status: 500, Message: "Documento Externo Capturado não se assina: use a autenticação"}
	}
	d.signed = true
	event := "Assinatura com senha"
	if captured {
		event = "Autenticação com senha"
	}
	m.addMov(d, event, "SEDEC", "SEDEC", mat, m.now())
	return nil
}

// Autuar transforma o documento assinado em processo e devolve o id.
func (m *Memory) Autuar(_ context.Context, tok Token, sigla string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("Autuar"); err != nil {
		return "", err
	}
	mat, err := m.auth(tok)
	if err != nil {
		return "", err
	}
	d := m.docs[sigla]
	if d == nil {
		return "", notFound()
	}
	if !d.signed {
		return "", &Error{Status: 500, Message: "Só se autua um documento assinado"}
	}
	if d.process {
		return "", &Error{Status: 500, Message: "Documento já está autuado"}
	}
	d.process = true
	m.addMov(d, "Autuação", "SEDEC", "SEDEC", mat, m.now())
	return d.sigla, nil
}

// Join junta um documento assinado a um processo.
func (m *Memory) Join(_ context.Context, tok Token, sigla, siglaPai string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("Join"); err != nil {
		return err
	}
	mat, err := m.auth(tok)
	if err != nil {
		return err
	}
	d, p := m.docs[sigla], m.docs[siglaPai]
	if d == nil || p == nil {
		return notFound()
	}
	if !d.signed {
		return &Error{Status: 500, Message: "Não é possível juntar documento não assinado"}
	}
	if !p.process {
		return &Error{Status: 500, Message: "Só se junta a um processo"}
	}
	if p.archived {
		return &Error{Status: 500, Message: "A via não pode ser juntada ao documento porque ele está arquivado"}
	}
	if d.parent != "" {
		return &Error{Status: 500, Message: "A via não pode ser juntada ao documento porque ele está juntado"}
	}
	d.parent = siglaPai
	m.addMov(p, "Juntada de "+d.description, "SEDEC", "SEDEC", mat, m.now())
	return nil
}

// FindByDescription procura um documento pela descrição exata.
func (m *Memory) FindByDescription(_ context.Context, tok Token, description string, _ time.Time) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("FindByDescription"); err != nil {
		return "", false, err
	}
	if _, err := m.auth(tok); err != nil {
		return "", false, err
	}
	var found []string
	for s, d := range m.docs {
		if d.description == description {
			found = append(found, s)
		}
	}
	sort.Strings(found)
	if len(found) == 0 {
		return "", false, nil
	}
	return found[0], true, nil
}

// Movements devolve as movimentações (da mais antiga para a mais nova).
func (m *Memory) Movements(_ context.Context, tok Token, sigla string) (MovementList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.enter("Movements"); err != nil {
		return MovementList{}, err
	}
	if _, err := m.auth(tok); err != nil {
		return MovementList{}, err
	}
	d := m.docs[sigla]
	if d == nil {
		return MovementList{}, notFound()
	}
	situation := "Aguardando andamento"
	if d.archived {
		situation = "Arquivado"
	}
	return MovementList{Sigla: sigla, Description: d.description, Situation: situation, Items: append([]Movement(nil), d.movs...)}, nil
}
