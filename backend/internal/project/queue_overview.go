package project

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
)

// Valores de scope das rotas da visão geral da equipe.
const (
	QueueScopeTeam = "team" // todos os projetos enviados que a pessoa enxerga
	QueueScopeMine = "mine" // só os que a pessoa assumiu (analyst_id)

	queueBlockItems = 15
	// defaultReadyCandidates é quantos projetos em análise e sem departamento (os mais antigos) têm a conferência calculada para o bloco
	// "pronto para indicar": o cálculo roda a validação de cada um, então é limitado (ver SetReadyCandidates).
	defaultReadyCandidates = 30
	deadlineWindow         = 48 * time.Hour
	archivedWindowDays     = 30
)

// Etapas de trabalho: o estado do projeto visto de quem está com ele. "Aguardando proponente" não tem etapa de trabalho (tem o prazo da
// complementação), e rascunho e arquivado também não.
const (
	workAwaitingChecklist = "awaiting_checklist" // com a equipe, na fila de entrada
	workInAnalysis        = "in_analysis"        // com o Analista
	workFraming           = "framing"            // enquadramento pendente, com o departamento
	workAwaitingOpinion   = "awaiting_opinion"   // enquadrado, aguardando parecer
)

// workSQL é a etapa de trabalho de uma linha de projetos. Espelha holderOf (history.go), com o enquadramento pendente separado da análise.
const workSQL = `(CASE WHEN status <> 'submitted' THEN NULL
	WHEN framing_status = 'pending' THEN '` + workFraming + `'
	WHEN stage = 'awaiting_checklist' THEN '` + workAwaitingChecklist + `'
	WHEN stage = 'in_analysis' THEN '` + workInAnalysis + `'
	WHEN stage = 'awaiting_opinion' THEN '` + workAwaitingOpinion + `'
	END)`

// sinceSQL é há quantos segundos o projeto está com o responsável atual: a duração do trecho aberto do histórico de etapas.
const sinceSQL = `(SELECT EXTRACT(EPOCH FROM now() - h.entered_at)::bigint FROM project_stage_history h
	WHERE h.project_id = projects.id AND h.left_at IS NULL)`

// QueueCounts conta os projetos por etapa. O enquadramento pendente conta só em FramingPending (não em InAnalysis).
type QueueCounts struct {
	AwaitingChecklist int `json:"awaiting_checklist"`
	InAnalysis        int `json:"in_analysis"`
	FramingPending    int `json:"framing_pending"`
	AwaitingProponent int `json:"awaiting_proponent"`
	AwaitingOpinion   int `json:"awaiting_opinion"`
	// Archived30d são os arquivados nos últimos 30 dias; o total completo fica na aba "Arquivados" da fila.
	Archived30d int `json:"archived_30d"`
}

// QueueItem é um projeto num bloco da visão geral. Os campos usados dependem do bloco.
type QueueItem struct {
	ProjectID   uuid.UUID `json:"projectId"`
	Title       string    `json:"title"`
	Code        string    `json:"code,omitempty"`
	Institution string    `json:"institution,omitempty"`
	// SinceSeconds é há quanto tempo o projeto está com o responsável atual (horas corridas).
	SinceSeconds int64 `json:"sinceSeconds"`
	Late         bool  `json:"late,omitempty"`
	OverLimit    bool  `json:"overLimit,omitempty"`
	// Stage (etapa de trabalho), Holder (team, analyst ou department) e LimitDays só vêm no bloco "parados".
	Stage     string       `json:"stage,omitempty"`
	Holder    string       `json:"holder,omitempty"`
	LimitDays int          `json:"limitDays,omitempty"`
	Analyst   *AnalystInfo `json:"analyst,omitempty"`
	// Department e RefusedAt só vêm no bloco "recusados".
	Department string     `json:"department,omitempty"`
	RefusedAt  *time.Time `json:"refusedAt,omitempty"`
	// DueAt e Overdue só vêm no bloco "prazos".
	DueAt   *time.Time `json:"dueAt,omitempty"`
	Overdue bool       `json:"overdue,omitempty"`
}

// QueueBlock é um bloco de trabalho: o total e os primeiros itens, do mais urgente para o menos.
type QueueBlock struct {
	Total int         `json:"total"`
	Items []QueueItem `json:"items"`
	// Capped: só vale para o bloco "pronto para indicar". Havia mais projetos candidatos que o limite, e Total conta só os prontos entre os
	// mais antigos. A tela mostra "N ou mais".
	Capped bool `json:"capped,omitempty"`
}

// QueueBlocks são os blocos "Precisa de você".
type QueueBlocks struct {
	Refused     QueueBlock `json:"refused"`
	Resubmitted QueueBlock `json:"resubmitted"`
	Entry       QueueBlock `json:"entry"`
	Ready       QueueBlock `json:"ready"`
	Stalled     QueueBlock `json:"stalled"`
	Deadlines   QueueBlock `json:"deadlines"`
}

// QueueOverview é a resposta de GET /admin/projects/queue/overview.
type QueueOverview struct {
	Counts QueueCounts `json:"counts"`
	// Limits são os dias vigentes de "parado" por etapa (zero = sem regra vigente; nada é marcado como parado).
	Limits rules.StalledAfterDays `json:"limits"`
	Blocks QueueBlocks            `json:"blocks"`
}

// queueQuery monta as consultas da visão geral sobre uma CTE "w" com os projetos do escopo e três colunas calculadas: work (etapa de
// trabalho), since (segundos com o responsável atual) e due_at (prazo da complementação aberta).
type queueQuery struct {
	where string
	args  []any
}

func (q *queueQuery) arg(v any) string {
	q.args = append(q.args, v)
	return "$" + strconv.Itoa(len(q.args))
}

func (q *queueQuery) cte() string {
	return `WITH w AS (SELECT projects.*, ` + workSQL + ` AS work, ` + sinceSQL + ` AS since,
		(SELECT c.due_at FROM project_complement_requests c WHERE c.project_id = projects.id AND c.status = 'open') AS due_at
		FROM projects` + q.where + `)`
}

// QueueOverview resume a fila do Analista: contagens por etapa e os blocos do que pede a ação dele. O alcance é a permissão da ação
// somada a "só projeto enviado ou arquivado" (rascunho é do proponente). scope=mine restringe aos projetos que a pessoa assumiu; o bloco
// "aguardando assumir" ignora o scope, porque ninguém é dono do projeto antes de assumir.
func (s *Service) QueueOverview(ctx context.Context, actor Actor, perm authz.Filter, scope string) (*QueueOverview, error) {
	snap, err := s.rules.SnapshotAt(ctx, s.q, s.today())
	if err != nil {
		return nil, err
	}
	out := &QueueOverview{Blocks: QueueBlocks{
		Refused: QueueBlock{Items: []QueueItem{}}, Resubmitted: QueueBlock{Items: []QueueItem{}}, Entry: QueueBlock{Items: []QueueItem{}}, Ready: QueueBlock{Items: []QueueItem{}},
		Stalled: QueueBlock{Items: []QueueItem{}}, Deadlines: QueueBlock{Items: []QueueItem{}},
	}}
	if snap.StalledAfter != nil {
		out.Limits = *snap.StalledAfter
	}

	conds, args, _, err := s.scopeConds(ctx, authz.And(perm, submittedFilter()), actor, "")
	if err != nil {
		return nil, err
	}
	// team: o alcance inteiro. scoped: o mesmo, mais "só os meus" quando pedido.
	team := func() *queueQuery { return &queueQuery{where: whereOf(conds), args: append([]any(nil), args...)} }
	scoped := func() *queueQuery {
		q := team()
		if scope == QueueScopeMine {
			q.where = whereOf(append(append([]string(nil), conds...), "analyst_id = "+q.arg(actor.ID)))
		}
		return q
	}

	if err := s.queueCounts(ctx, scoped(), out); err != nil {
		return nil, err
	}
	now := s.now()
	limitCase := func(q *queueQuery) string {
		return `(CASE work WHEN '` + workAwaitingChecklist + `' THEN ` + q.arg(out.Limits.AwaitingChecklist) + `::int` +
			` WHEN '` + workInAnalysis + `' THEN ` + q.arg(out.Limits.InAnalysis) + `::int` +
			` WHEN '` + workFraming + `' THEN ` + q.arg(out.Limits.Framing) + `::int` +
			` WHEN '` + workAwaitingOpinion + `' THEN ` + q.arg(out.Limits.AwaitingOpinion) + `::int END)`
	}

	// Recusados pelo Revisor: em análise, sem departamento, e a última rodada de enquadramento foi recusada.
	if err := s.queueBlock(ctx, scoped(), &out.Blocks.Refused, true, func(*queueQuery) (string, string) {
		return `work = '` + workInAnalysis + `' AND department_id IS NULL AND EXISTS (SELECT 1 FROM project_framings f
			WHERE f.project_id = w.id AND f.decision = 'refused'
			AND f.round = (SELECT max(f2.round) FROM project_framings f2 WHERE f2.project_id = w.id))`, `refused_at ASC NULLS LAST`
	}, nil); err != nil {
		return nil, err
	}
	// Reenviados pelo proponente: em análise, com o último reenvio sem nenhuma conferência depois dele. O trecho aberto vem do reenvio
	// ou de um analista que assumiu o projeto depois dele (a troca de responsável abre outro trecho, mas o reenvio segue sem conferir).
	if err := s.queueBlock(ctx, scoped(), &out.Blocks.Resubmitted, false, func(*queueQuery) (string, string) {
		return `work = '` + workInAnalysis + `' AND EXISTS (SELECT 1 FROM project_stage_history h
			WHERE h.project_id = w.id AND h.left_at IS NULL AND h.transition IN ('project.resubmit', 'project.take-over')
			AND EXISTS (SELECT 1 FROM project_stage_history r WHERE r.project_id = w.id AND r.transition = 'project.resubmit'
				AND r.entered_at = (SELECT max(r2.entered_at) FROM project_stage_history r2 WHERE r2.project_id = w.id AND r2.transition = 'project.resubmit')
				AND r.entered_at <= h.entered_at
				AND NOT EXISTS (SELECT 1 FROM audit_logs l WHERE l.entity = 'project' AND l.entity_id = w.id::text
					AND l.action = 'project.review-item' AND l.created_at >= r.entered_at)))`, `since DESC`
	}, nil); err != nil {
		return nil, err
	}
	// Aguardando assumir: a fila de entrada inteira, mesmo em "só os meus".
	if err := s.queueBlock(ctx, team(), &out.Blocks.Entry, false, func(*queueQuery) (string, string) {
		return `work = '` + workAwaitingChecklist + `'`, `since DESC`
	}, func(it *QueueItem) {
		it.OverLimit = out.Limits.AwaitingChecklist > 0 && it.SinceSeconds >= int64(out.Limits.AwaitingChecklist)*86400
	}); err != nil {
		return nil, err
	}
	// Pronto para indicar o departamento: em análise, sem departamento e com tudo aprovado.
	if err := s.queueReady(ctx, scoped(), actor, &out.Blocks.Ready); err != nil {
		return nil, err
	}
	// Parados: o tempo com o responsável atual passou do limite da etapa. Mais atrasado (proporcional ao limite) primeiro.
	if out.Limits != (rules.StalledAfterDays{}) {
		if err := s.queueBlock(ctx, scoped(), &out.Blocks.Stalled, false, func(q *queueQuery) (string, string) {
			lim := limitCase(q)
			return `work IS NOT NULL AND since >= ` + lim + ` * 86400`, `since / (` + lim + ` * 86400.0) DESC`
		}, func(it *QueueItem) {
			it.Holder, it.LimitDays = holderOfWork(it.Stage), limitOfWork(out.Limits, it.Stage)
		}); err != nil {
			return nil, err
		}
	}
	// Prazos a vencer: complementação aberta que vence nas próximas 48 horas (ou já venceu e o laço periódico ainda não arquivou).
	if err := s.queueBlock(ctx, scoped(), &out.Blocks.Deadlines, false, func(q *queueQuery) (string, string) {
		return `stage = 'awaiting_proponent' AND due_at IS NOT NULL AND due_at <= ` + q.arg(now.Add(deadlineWindow)), `due_at ASC`
	}, func(it *QueueItem) {
		it.Overdue = it.DueAt != nil && it.DueAt.Before(now)
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) queueCounts(ctx context.Context, q *queueQuery, out *QueueOverview) error {
	rows, err := s.pool.Query(ctx, `SELECT status, COALESCE(stage, ''), COALESCE(framing_status, ''), count(*),
		count(*) FILTER (WHERE status = 'archived' AND archived_at >= now() - make_interval(days => `+strconv.Itoa(archivedWindowDays)+`))
		FROM projects`+q.where+` GROUP BY 1, 2, 3`, q.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status, stage, framing string
		var n, recent int
		if err := rows.Scan(&status, &stage, &framing, &n, &recent); err != nil {
			return err
		}
		c := &out.Counts
		switch {
		case status == StatusArchived:
			c.Archived30d += recent
		case status != StatusSubmitted:
		case framing == FramingPending:
			c.FramingPending += n
		case stage == StageAwaitingChecklist:
			c.AwaitingChecklist += n
		case stage == StageInAnalysis:
			c.InAnalysis += n
		case stage == StageAwaitingProponent:
			c.AwaitingProponent += n
		case stage == StageAwaitingOpinion:
			c.AwaitingOpinion += n
		}
	}
	return rows.Err()
}

// queueBlock preenche um bloco: where e orderBy definem quem entra e em que ordem; decorate acerta os campos de cada bloco. Só o bloco dos
// recusados (refusals) leva o departamento e a data da recusa: nos outros, a rodada recusada de antes não diz nada do estado de hoje.
func (s *Service) queueBlock(ctx context.Context, q *queueQuery, b *QueueBlock, refusals bool, build func(*queueQuery) (where, orderBy string), decorate func(*QueueItem)) error {
	items, total, err := s.queueItems(ctx, q, refusals, queueBlockItems, build, decorate)
	if err != nil {
		return err
	}
	b.Items, b.Total = append(b.Items, items...), total
	return nil
}

// queueItems devolve até limit projetos que casam com o where, na ordem pedida, e o total que casa (sem o limite).
func (s *Service) queueItems(ctx context.Context, q *queueQuery, refusals bool, limit int, build func(*queueQuery) (where, orderBy string), decorate func(*QueueItem)) ([]QueueItem, int, error) {
	where, order := build(q)
	rows, err := s.pool.Query(ctx, q.cte()+` SELECT id, title, COALESCE(code, ''), institution_name, late_submission, COALESCE(since, 0), COALESCE(work, ''),
		analyst_id, (SELECT NULLIF(trim(a.first_name || ' ' || a.last_name), '') FROM admins a WHERE a.id = w.analyst_id), due_at,
		COALESCE((SELECT f.department_name FROM project_framings f WHERE f.project_id = w.id AND f.decision = 'refused' ORDER BY f.round DESC LIMIT 1), ''),
		(SELECT f.decided_at FROM project_framings f WHERE f.project_id = w.id AND f.decision = 'refused' ORDER BY f.round DESC LIMIT 1) AS refused_at,
		count(*) OVER ()
		FROM w WHERE `+where+` ORDER BY `+order+`, id LIMIT `+strconv.Itoa(limit), q.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []QueueItem
	total := 0
	for rows.Next() {
		var it QueueItem
		var anID *uuid.UUID
		var anName *string
		if err := rows.Scan(&it.ProjectID, &it.Title, &it.Code, &it.Institution, &it.Late, &it.SinceSeconds, &it.Stage, &anID, &anName,
			&it.DueAt, &it.Department, &it.RefusedAt, &total); err != nil {
			return nil, 0, err
		}
		if anID != nil {
			it.Analyst = &AnalystInfo{ID: *anID, Name: str(anName)}
		}
		if !refusals {
			it.Department, it.RefusedAt = "", nil
		}
		if decorate != nil {
			decorate(&it)
		}
		items = append(items, it)
	}
	return items, total, rows.Err()
}

// queueReady monta o bloco "pronto para indicar o departamento": em análise, sem departamento e com todos os itens aprovados. A
// conferência é calculada em Go (depende do checklist do projeto), só para os readyCandidates mais antigos. Um projeto que falha no
// cálculo é registrado no log e sai do bloco, sem derrubar a resposta.
func (s *Service) queueReady(ctx context.Context, q *queueQuery, actor Actor, b *QueueBlock) error {
	cands, total, err := s.queueItems(ctx, q, false, s.readyLimit(), func(*queueQuery) (string, string) {
		return `work = '` + workInAnalysis + `' AND department_id IS NULL`, `since DESC`
	}, nil)
	if err != nil {
		return err
	}
	b.Capped = total > len(cands)
	for _, it := range cands {
		row, err := s.q.GetProject(ctx, it.ProjectID)
		if err == nil {
			var cl *Checklist
			if _, cl, err = s.viewWithChecklist(ctx, s.q, row, actor); err == nil {
				var rv Review
				if rv, err = s.reviewState(ctx, s.q, it.ProjectID, cl, nil); err == nil && !rv.AllApproved {
					continue
				}
			}
		}
		if err != nil {
			if s.log != nil {
				s.log.ErrorContext(ctx, "calculando a conferência na visão geral da fila", "project", it.ProjectID, "error", err)
			}
			continue
		}
		b.Total++
		if len(b.Items) < queueBlockItems {
			b.Items = append(b.Items, it)
		}
	}
	return nil
}

// holderOfWork diz com quem o projeto está em cada etapa de trabalho.
func holderOfWork(work string) string {
	switch work {
	case workAwaitingChecklist:
		return HolderTeam
	case workInAnalysis:
		return HolderAnalyst
	case workFraming, workAwaitingOpinion:
		return HolderDepartment
	}
	return ""
}

func limitOfWork(l rules.StalledAfterDays, work string) int {
	switch work {
	case workAwaitingChecklist:
		return l.AwaitingChecklist
	case workInAnalysis:
		return l.InAnalysis
	case workFraming:
		return l.Framing
	case workAwaitingOpinion:
		return l.AwaitingOpinion
	}
	return 0
}

// SetReadyCandidates muda quantos projetos têm a conferência calculada no bloco "pronto para indicar" (padrão 30). Existe para os testes
// poderem exercer o limite sem criar dezenas de projetos.
func (s *Service) SetReadyCandidates(n int) { s.readyCandidates = n }

func (s *Service) readyLimit() int {
	if s.readyCandidates > 0 {
		return s.readyCandidates
	}
	return defaultReadyCandidates
}
