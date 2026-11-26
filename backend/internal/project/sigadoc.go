package project

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/sigadoc"
)

// Acompanhamento da tramitação do processo no SIGADOC (docs/planejamento-integracao-sigadoc.md, seção 6.4). Depois que o número do
// processo é gravado, o sistema consulta o processo de tempos em tempos e registra cada movimentação nova no histórico do projeto.
// Só lê: parecer, despachos e tramitações são feitos no SIGADOC. Termina quando o processo é arquivado também no SIGADOC.

const (
	actionSigadocSeen = "project.sigadoc-movements-seen"
	// watchBase é o intervalo entre consultas quando há novidade; sem novidade ele dobra até watchMax.
	watchBase = 15 * time.Minute
	watchMax  = 6 * time.Hour
	// watchBatch é quantos processos uma rodada consulta, e watchEvery de quanto em quanto tempo a rodada roda.
	watchBatch = 20
	watchEvery = time.Minute
	// maxWatchError limita a mensagem de erro guardada.
	maxWatchError = 300
)

// SetSigadoc liga o SIGADOC. Sem ele, o acompanhamento não roda e a abertura do processo responde 503.
func (s *Service) SetSigadoc(c sigadoc.Client) {
	if c == nil {
		s.sigadoc = nil
		return
	}
	s.sigadoc = sigadoc.NewSession(c)
}

// SigadocProcess é o processo do projeto no SIGADOC.
type SigadocProcess struct {
	Number string    `json:"number"`
	Sigla  string    `json:"sigla"`
	Opened time.Time `json:"openedAt"`
	// ArchivedAtSigadoc só existe quando o acompanhamento viu o processo arquivado no SIGADOC.
	ArchivedAtSigadoc *time.Time `json:"archivedAtSigadoc"`
	Situation         string     `json:"situation"`
	LastCheckedAt     *time.Time `json:"lastCheckedAt"`
	// Error é o erro curto da última consulta (vazio se deu certo).
	Error string `json:"error"`
}

// SigadocMovement é uma movimentação do processo vista no SIGADOC.
type SigadocMovement struct {
	ID          uuid.UUID  `json:"id"`
	MovID       int64      `json:"movId"`
	Event       string     `json:"event"`
	Description string     `json:"description"`
	Lotacao     string     `json:"lotacao"`
	Destination string     `json:"destination"`
	Person      string     `json:"person"`
	At          *time.Time `json:"at"`
	AtRaw       string     `json:"atRaw"`
	// AfterArchive: aconteceu depois de o projeto ser arquivado no sistema. Seen: a equipe já marcou como vista.
	AfterArchive bool `json:"afterArchive"`
	Seen         bool `json:"seen"`
}

// SigadocView é o que a equipe vê do SIGADOC no projeto.
type SigadocView struct {
	Process   *SigadocProcess   `json:"process"`
	Movements []SigadocMovement `json:"movements"`
	// ProjectArchived: o projeto está arquivado no sistema. StillOpenAtSigadoc: e o processo segue sem arquivamento no SIGADOC (o
	// aviso chamativo da tela). NewAfterArchive conta as movimentações posteriores ao arquivamento que a equipe ainda não viu.
	ProjectArchived    bool `json:"projectArchived"`
	StillOpenAtSigadoc bool `json:"stillOpenAtSigadoc"`
	NewAfterArchive    int  `json:"newAfterArchive"`
	// Task é a última tentativa de montagem do processo (nula se nunca houve); Opening diz se o botão "Abrir no SIGADOC" cabe.
	Task    *SigadocTask   `json:"task"`
	Opening SigadocOpening `json:"opening"`
}

// sigadocForStaff confere que a pessoa é da equipe e enxerga o projeto (como o histórico de etapas: o proponente recebe 404).
func (s *Service) sigadocForStaff(ctx context.Context, q *db.Queries, actor Actor, id uuid.UUID, read authz.Decision) (db.Project, error) {
	r, err := q.GetProject(ctx, id)
	if err != nil {
		return db.Project{}, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) {
		return db.Project{}, httpapi.ErrNotFound()
	}
	access, err := s.accessOf(ctx, q, r.ID, r.CreatedByID, actor)
	if err != nil {
		return db.Project{}, err
	}
	if access != "" {
		return db.Project{}, httpapi.ErrNotFound()
	}
	return r, nil
}

// SigadocState devolve o processo do projeto no SIGADOC e as movimentações vistas, para a equipe. Sem processo, Process é nulo.
func (s *Service) SigadocState(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) (*SigadocView, error) {
	r, err := s.sigadocForStaff(ctx, s.q, actor, id, read)
	if err != nil {
		return nil, err
	}
	return s.sigadocView(ctx, s.q, r)
}

func (s *Service) sigadocView(ctx context.Context, q *db.Queries, r db.Project) (*SigadocView, error) {
	out := &SigadocView{Movements: []SigadocMovement{}, ProjectArchived: r.Status == StatusArchived}
	task, err := s.sigadocTaskView(ctx, q, r.ID)
	if err != nil {
		return nil, err
	}
	out.Task = task
	p, err := q.GetProjectSigadoc(ctx, r.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		out.Opening = s.sigadocOpening(ctx, q, r, false, task)
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Opening = s.sigadocOpening(ctx, q, r, true, task)
	out.Process = &SigadocProcess{Number: p.ProcessNumber, Sigla: p.ProcessSigla, Opened: p.OpenedAt, ArchivedAtSigadoc: p.SigadocArchivedAt,
		Situation: p.Situation, LastCheckedAt: p.WatchedAt, Error: p.WatchError}
	rows, err := q.ListSigadocMovements(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		out.Movements = append(out.Movements, SigadocMovement{ID: m.ID, MovID: m.MovID, Event: m.Event, Description: m.DocumentDescription, Lotacao: m.Lotacao,
			Destination: m.Destination, Person: m.Person, At: m.PerformedAt, AtRaw: m.PerformedAtRaw, AfterArchive: m.AfterProjectArchive, Seen: m.SeenAt != nil})
		if m.AfterProjectArchive && m.SeenAt == nil {
			out.NewAfterArchive++
		}
	}
	out.StillOpenAtSigadoc = out.ProjectArchived && p.SigadocArchivedAt == nil
	return out, nil
}

// MarkSigadocSeen marca como vistas as movimentações posteriores ao arquivamento (a equipe viu o aviso). Fica na auditoria.
func (s *Service) MarkSigadocSeen(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) (*SigadocView, error) {
	var out *SigadocView
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		r, err := s.sigadocForStaff(ctx, q, actor, id, read)
		if err != nil {
			return err
		}
		n, err := q.MarkSigadocMovementsSeen(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			if err := s.audit.Record(ctx, q, actor.entry(actionSigadocSeen, id, map[string]any{"count": n})); err != nil {
				return err
			}
		}
		out, err = s.sigadocView(ctx, q, r)
		return err
	})
	return out, err
}

// RegisterSigadocProcess grava o processo do projeto no SIGADOC (número e sigla imutáveis). É chamado pela montagem do processo, na
// transação que muda a etapa; q deve estar ligada a ela.
func (s *Service) RegisterSigadocProcess(ctx context.Context, q *db.Queries, projectID uuid.UUID, matricula, number, sigla string, openedBy *uuid.UUID) error {
	_, err := q.CreateProjectSigadoc(ctx, db.CreateProjectSigadocParams{ProjectID: projectID, Matricula: matricula, ProcessNumber: number, ProcessSigla: sigla, OpenedByID: openedBy})
	return err
}

// RunSigadocWatcher consulta os processos até o contexto terminar. Várias instâncias da API podem rodá-lo: cada processo é reservado
// por quem o consulta (ClaimSigadocWatches). Sem SIGADOC ligado, não faz nada.
func (s *Service) RunSigadocWatcher(ctx context.Context) {
	if s.sigadoc == nil {
		return
	}
	t := time.NewTicker(watchEvery)
	defer t.Stop()
	for {
		if n, err := s.ProcessSigadocWatches(ctx); err != nil {
			s.logger().ErrorContext(ctx, "acompanhando processos no SIGADOC", "error", err)
		} else if n > 0 {
			s.logger().DebugContext(ctx, "processos do SIGADOC consultados", "quantidade", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ProcessSigadocWatches consulta os processos com a consulta vencida e devolve quantos reservou. Uma falha num processo não impede os outros.
func (s *Service) ProcessSigadocWatches(ctx context.Context) (int, error) {
	if s.sigadoc == nil {
		return 0, nil
	}
	rows, err := s.q.ClaimSigadocWatches(ctx, watchBatch)
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		s.watchOne(ctx, row)
	}
	return len(rows), nil
}

// nextInterval: com novidade volta ao mínimo; sem novidade dobra, até o teto.
func nextInterval(prev int32, news bool) int32 {
	base, maxi := int32(watchBase/time.Second), int32(watchMax/time.Second)
	if news || prev < base {
		return base
	}
	return min(prev*2, maxi)
}

func (s *Service) watchOne(ctx context.Context, row db.ProjectSigadoc) {
	log := s.logger().With("projectId", row.ProjectID, "sigla", row.ProcessSigla)
	fail := func(err error) {
		msg := watchErrorText(err)
		log.WarnContext(ctx, "consulta ao SIGADOC falhou", "error", msg)
		if e := s.q.FailSigadocWatch(ctx, db.FailSigadocWatchParams{ProjectID: row.ProjectID, IntervalSeconds: nextInterval(row.WatchIntervalSeconds, false), Error: msg}); e != nil {
			log.ErrorContext(ctx, "gravando a falha da consulta", "error", e)
		}
	}
	tok, err := s.sigadoc.Token(ctx, row.Matricula)
	if err != nil {
		fail(err)
		return
	}
	list, err := s.sigadoc.Client().Movements(ctx, tok, row.ProcessSigla)
	if err != nil {
		var se *sigadoc.Error
		if errors.As(err, &se) && se.Status == 401 {
			s.sigadoc.Forget(row.Matricula)
		}
		fail(err)
		return
	}
	news := 0
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		proj, err := q.GetProject(ctx, row.ProjectID)
		if err != nil {
			return err
		}
		for _, m := range list.Items {
			var at *time.Time
			after := false
			if !m.At.IsZero() {
				t := m.At
				at = &t
				// Depois do arquivamento no sistema: só dá para afirmar com a data da movimentação.
				after = proj.Status == StatusArchived && proj.ArchivedAt != nil && m.At.After(*proj.ArchivedAt)
			}
			n, err := q.InsertSigadocMovement(ctx, db.InsertSigadocMovementParams{ProjectID: row.ProjectID, MovID: m.ID, TypeID: smallInt(m.TypeID),
				Event: m.Event, DocumentDescription: list.Description, Lotacao: m.Lotacao, Destination: m.Destination, Person: m.Person,
				PerformedAt: at, PerformedAtRaw: m.RawDate, AfterProjectArchive: after})
			if err != nil {
				return err
			}
			news += int(n)
		}
		return q.FinishSigadocWatch(ctx, db.FinishSigadocWatchParams{ProjectID: row.ProjectID, Situation: truncate(list.Situation, maxWatchError),
			IntervalSeconds: nextInterval(row.WatchIntervalSeconds, news > 0), Archived: list.IsArchived()})
	})
	if err != nil {
		fail(err)
		return
	}
	if news > 0 {
		log.InfoContext(ctx, "movimentações novas no SIGADOC", "quantidade", news, slog.Bool("arquivadoNoSigadoc", list.IsArchived()))
	}
}

// watchErrorText é o texto curto que fica guardado e aparece na tela: a mensagem do SIGADOC (sem stacktrace) ou um aviso genérico de
// rede. Nunca leva URL, token, senha nem PIN.
func watchErrorText(err error) string {
	var se *sigadoc.Error
	if errors.As(err, &se) {
		if sigadoc.IsGatewayError(err) {
			return gatewayText
		}
		if sigadoc.IsProhibited(err) {
			return truncate(se.Message, maxWatchError) + prohibitedHint
		}
		if sigadoc.IsForbidden(err) {
			return "O usuário de sistema não tem acesso a este método do SIGADOC."
		}
		return truncate(se.Message, maxWatchError)
	}
	if sigadoc.IsAuthFailed(err) || sigadoc.IsUnknownIdentity(err) {
		return "O SIGADOC recusou a autenticação."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "O SIGADOC demorou demais para responder."
	}
	return "Não foi possível consultar o SIGADOC."
}

// smallInt cabe o tipo da movimentação (um número pequeno do SIGADOC) numa coluna de 32 bits; fora da faixa vira zero.
func smallInt(n int) int32 {
	if n < 0 || n > math.MaxInt32 {
		return 0
	}
	return int32(n)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
