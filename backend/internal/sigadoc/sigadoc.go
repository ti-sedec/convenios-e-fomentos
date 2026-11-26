package sigadoc

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// PIN é o PIN de assinatura do analista. Ele só existe em memória, durante a requisição ou a tarefa que o usa: nunca vai para banco,
// fila, cache, log, auditoria ou mensagem de erro. O tipo se recusa a ser impresso (String, GoString e LogValue devolvem um
// marcador), e Zero apaga os bytes quando acaba o uso.
type PIN []byte

// NewPIN copia o texto para um buffer que pode ser zerado depois.
func NewPIN(s string) PIN { return PIN(append([]byte(nil), s...)) }

// Zero apaga o PIN da memória.
func (p PIN) Zero() {
	for i := range p {
		p[i] = 0
	}
}

// String impede que o PIN apareça em %s, %v ou %q.
func (PIN) String() string { return "[PIN]" }

// GoString impede que o PIN apareça em %#v.
func (PIN) GoString() string { return "sigadoc.PIN([PIN])" }

// LogValue impede que o PIN apareça no slog.
func (PIN) LogValue() slog.Value { return slog.StringValue("[PIN]") }

// Client é o contrato do SIGADOC que o sistema usa. Todas as chamadas levam o token da matrícula que está sendo representada
// (veja Authenticate): é por ele que o SIGADOC sabe em nome de quem agir e quem assina.
type Client interface {
	// Authenticate devolve o token (JWT) do usuário de sistema representando a matrícula.
	Authenticate(ctx context.Context, matricula string) (Token, error)
	// MatriculaByCPF devolve as matrículas do CPF no órgão (ex.: SEDEC).
	MatriculaByCPF(ctx context.Context, tok Token, cpf, orgao string) ([]string, error)

	// PreviewTermo gera o HTML do modelo preenchido (nada é gravado no SIGADOC). modelID é o id numérico do modelo.
	PreviewTermo(ctx context.Context, tok Token, modelID string, fields map[string]string) (string, error)
	// ValidatePIN confere o PIN do dono do CPF sem assinar nada.
	ValidatePIN(ctx context.Context, tok Token, cpf string, pin PIN) error

	// CreateDocument cria um documento (do modelo, ou capturado, com o PDF) e devolve a sigla.
	CreateDocument(ctx context.Context, tok Token, in NewDocument) (string, error)
	// Finalize finaliza a elaboração do documento e devolve a sigla DEFINITIVA, sem caracteres especiais. O número que a criação devolve
	// ("TMP-…") é provisório: depois de finalizar, as demais chamadas (assinar, autenticar, autuar, juntar) usam a sigla devolvida aqui.
	Finalize(ctx context.Context, tok Token, sigla string) (string, error)
	// Sign assina o documento com o PIN: o produzido no SIGADOC (o Termo) e o Interno Capturado (o relatório).
	Sign(ctx context.Context, tok Token, sigla string, pin PIN) error
	// AutenticarDocumento autentica com o PIN o documento Externo Capturado (os arquivos do proponente): comprova que é cópia fiel do
	// original. O Interno Capturado não se autentica, só se assina (informado pela MTI).
	AutenticarDocumento(ctx context.Context, tok Token, sigla string, pin PIN) error
	// Autuar autua o documento e devolve o id que o SIGADOC informa.
	Autuar(ctx context.Context, tok Token, sigla string) (string, error)
	// Join junta o documento ao processo (siglaPai é a via ou o volume do processo).
	Join(ctx context.Context, tok Token, sigla, siglaPai string) error

	// FindByDescription procura, entre os documentos criados desde since pela matrícula do token, um com esta descrição exata. Serve para
	// reconhecer um documento criado antes de uma queda, em vez de criar outro. O cliente real ainda não sabe fazer isso
	// (ErrUnsupported): o formato da pesquisa do SIGADOC não está documentado.
	FindByDescription(ctx context.Context, tok Token, description string, since time.Time) (sigla string, found bool, err error)

	// Movements consulta o documento e a lista das suas movimentações (só leitura).
	Movements(ctx context.Context, tok Token, sigla string) (MovementList, error)
}

// ErrUnsupported indica uma operação que este cliente ainda não implementa.
var ErrUnsupported = errors.New("sigadoc: operação não suportada por este cliente")

// NewDocument são os dados de um documento a criar.
type NewDocument struct {
	// Model é o id ou o nome do modelo.
	Model string
	// DocType é a "descrição do tipo de documento": "Interno Capturado", "Externo Capturado"...
	DocType     string
	Description string
	// Subscriber é a matrícula de quem assina o documento (campo "subscritor"). O SIGADOC recusa finalizar e assinar sem subscritor.
	Subscriber string
	// Interview são os campos da entrevista do modelo (documento que não é capturado).
	Interview      map[string]string
	Classification string
	AccessLevel    string
	// DestinationLotacao é a sigla da lotação de destino, se houver.
	DestinationLotacao string
	Electronic         bool
	// File e Filename são o PDF de um documento capturado.
	File     []byte
	Filename string
}

// Movement é uma movimentação do documento no SIGADOC.
type Movement struct {
	ID     int64
	TypeID int
	// Event é a descrição do evento ("Juntada", "Tramitação", "Arquivamento corrente"...).
	Event string
	// Lotacao é a lotação de quem fez; Destination, a que atendeu ou recebeu; Person, quem atendeu.
	Lotacao, Destination, Person string
	// At é a data da movimentação, vazia se o formato não foi reconhecido (veja RawDate).
	At      time.Time
	RawDate string
}

// MovementList é o documento e as suas movimentações.
type MovementList struct {
	Sigla       string
	Description string
	// Situation é a situação do documento, como o SIGADOC a informa.
	Situation string
	Items     []Movement
}

// IsArchived diz se o documento está arquivado. O SIGADOC não tem um campo booleano nesta consulta (o método
// consultar-arquivado está bloqueado para o usuário de sistema): reconhece-se pela situação e pela última movimentação cujo evento
// começa com "Arquivamento" ou com "Desarquivamento". É uma dedução (docs/planejamento-integracao-sigadoc.md, P5).
func (l MovementList) IsArchived() bool {
	if s := strings.ToLower(l.Situation); strings.Contains(s, "arquivado") && !strings.Contains(s, "desarquiv") {
		return true
	}
	archived := false
	items := append([]Movement(nil), l.Items...)
	// Em ordem crescente de id, a última movimentação de arquivamento ou de desarquivamento decide.
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, m := range items {
		// O evento precisa COMEÇAR assim: "Juntada de Certidão de Arquivamento" não é um arquivamento do processo.
		e := strings.ToLower(strings.TrimSpace(m.Event))
		switch {
		case strings.HasPrefix(e, "desarquiv"):
			archived = false
		case strings.HasPrefix(e, "arquiv"):
			archived = true
		}
	}
	return archived
}

// Session guarda o token de cada matrícula até perto do vencimento, para não autenticar a cada chamada.
type Session struct {
	c      Client
	margin time.Duration
	mu     sync.Mutex
	tokens map[string]Token
}

// NewSession cria a sessão sobre c. O token é renovado quando faltar menos de 5 minutos para vencer.
func NewSession(c Client) *Session {
	return &Session{c: c, margin: 5 * time.Minute, tokens: map[string]Token{}}
}

// Client devolve o cliente por baixo.
func (s *Session) Client() Client { return s.c }

// Token devolve o token da matrícula, autenticando se não houver um válido.
func (s *Session) Token(ctx context.Context, matricula string) (Token, error) {
	matricula = strings.ToUpper(strings.TrimSpace(matricula))
	s.mu.Lock()
	t, ok := s.tokens[matricula]
	s.mu.Unlock()
	if ok && t.Valid(s.margin) {
		return t, nil
	}
	t, err := s.c.Authenticate(ctx, matricula)
	if err != nil {
		return Token{}, err
	}
	s.mu.Lock()
	s.tokens[matricula] = t
	s.mu.Unlock()
	return t, nil
}

// Forget esquece o token da matrícula (por exemplo, depois de um 401).
func (s *Session) Forget(matricula string) {
	s.mu.Lock()
	delete(s.tokens, strings.ToUpper(strings.TrimSpace(matricula)))
	s.mu.Unlock()
}
