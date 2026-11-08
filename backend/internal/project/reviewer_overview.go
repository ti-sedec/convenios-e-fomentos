package project

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// reviewerBlockItems é quantos enquadramentos pendentes a visão geral do Revisor traz (os mais antigos).
const reviewerBlockItems = 15

// ReviewerDepartment é o departamento do cadastro do Revisor.
type ReviewerDepartment struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ReviewerLimits são os dias vigentes de "parado" que interessam ao Revisor (parâmetro stalled_after_days). Zero = sem regra vigente.
type ReviewerLimits struct {
	Framing         int `json:"framing"`
	AwaitingOpinion int `json:"awaiting_opinion"`
}

// ReviewerDecided são as decisões do departamento num período, por rodada de enquadramento.
type ReviewerDecided struct {
	Confirmed int `json:"confirmed"`
	Refused   int `json:"refused"`
}

// ReviewerCounts são os números do departamento. O arquivado conta só nos últimos 30 dias; o total completo fica na fila.
type ReviewerCounts struct {
	FramingPending  int             `json:"framing_pending"`
	AwaitingOpinion int             `json:"awaiting_opinion"`
	Archived30d     int             `json:"archived_30d"`
	Decided30d      ReviewerDecided `json:"decided_30d"`
}

// ReviewerItem é um enquadramento que espera a decisão do departamento.
type ReviewerItem struct {
	ProjectID   uuid.UUID `json:"projectId"`
	Title       string    `json:"title"`
	Code        string    `json:"code,omitempty"`
	Institution string    `json:"institution,omitempty"`
	// Round, IndicatedAt e IndicatedBy são da rodada pendente: quando e por quem o departamento foi indicado.
	Round       int       `json:"round"`
	IndicatedAt time.Time `json:"indicatedAt"`
	IndicatedBy string    `json:"indicatedBy,omitempty"`
	// SinceSeconds é há quanto tempo o projeto está com o departamento (trecho aberto do histórico de etapas).
	SinceSeconds int64 `json:"sinceSeconds"`
	Late         bool  `json:"late,omitempty"`
	// OverLimit: passou do limite de "parado" do enquadramento.
	OverLimit bool `json:"overLimit,omitempty"`
}

// ReviewerBlock é o bloco "aguardando a sua decisão": o total e os primeiros itens, do mais antigo para o mais novo.
type ReviewerBlock struct {
	Total int            `json:"total"`
	Items []ReviewerItem `json:"items"`
}

// ReviewerOverview é a resposta de GET /admin/projects/department/overview.
type ReviewerOverview struct {
	// Department é nulo quando a pessoa não tem departamento no cadastro: nada é consultado e o resto vem zerado.
	Department *ReviewerDepartment `json:"department"`
	Limits     ReviewerLimits      `json:"limits"`
	Counts     ReviewerCounts      `json:"counts"`
	Pending    ReviewerBlock       `json:"pending"`
}

// ReviewerOverview resume o trabalho do Revisor: os enquadramentos do departamento dele que esperam decisão e os números do
// departamento. O alcance é a permissão da ação somada a "só projeto enviado ou arquivado" e, por garantia, ao departamento do
// cadastro da pessoa (quem passa por qualquer permissão, como o Super Admin, não vê os de outros departamentos por esta rota).
// Quem não tem departamento recebe tudo zerado e Department nulo.
func (s *Service) ReviewerOverview(ctx context.Context, actor Actor, perm authz.Filter) (*ReviewerOverview, error) {
	out := &ReviewerOverview{Pending: ReviewerBlock{Items: []ReviewerItem{}}}
	snap, err := s.rules.SnapshotAt(ctx, s.q, s.today())
	if err != nil {
		return nil, err
	}
	if snap.StalledAfter != nil {
		out.Limits = ReviewerLimits{Framing: snap.StalledAfter.Framing, AwaitingOpinion: snap.StalledAfter.AwaitingOpinion}
	}

	dept, err := s.reviewerDepartment(ctx, actor)
	if err != nil || dept == nil {
		return out, err
	}
	out.Department = dept

	conds, args, _, err := s.scopeConds(ctx, authz.And(perm, submittedFilter()), actor, "")
	if err != nil {
		return nil, err
	}
	q := &queueQuery{args: args}
	q.where = whereOf(append(conds, "department_id = "+q.arg(dept.ID)))

	if err := s.reviewerCounts(ctx, q, dept.ID, out); err != nil {
		return nil, err
	}
	return out, s.reviewerPending(ctx, q, out)
}

// reviewerDepartment é o departamento do cadastro da pessoa (nulo se não tem). As rotas do Revisor usam sempre este departamento, e não
// só a condição da permissão: quem passa por qualquer permissão (Super Admin) e não tem departamento recebe tudo vazio.
func (s *Service) reviewerDepartment(ctx context.Context, actor Actor) (*ReviewerDepartment, error) {
	var dept ReviewerDepartment
	err := s.pool.QueryRow(ctx, `SELECT d.id, d.name FROM admins a JOIN departments d ON d.id = a.department_id WHERE a.id = $1`, actor.ID).Scan(&dept.ID, &dept.Name)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &dept, nil
}

// reviewerCounts conta os projetos do departamento por situação e as decisões dos últimos 30 dias.
func (s *Service) reviewerCounts(ctx context.Context, q *queueQuery, dept uuid.UUID, out *ReviewerOverview) error {
	rows, err := s.pool.Query(ctx, `SELECT status, COALESCE(framing_status, ''), count(*),
		count(*) FILTER (WHERE status = 'archived' AND archived_at >= now() - make_interval(days => `+strconv.Itoa(archivedWindowDays)+`))
		FROM projects`+q.where+` GROUP BY 1, 2`, q.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var status, framing string
		var n, recent int
		if err := rows.Scan(&status, &framing, &n, &recent); err != nil {
			return err
		}
		c := &out.Counts
		switch {
		case status == StatusArchived:
			c.Archived30d += recent
		case status != StatusSubmitted:
		case framing == FramingPending:
			c.FramingPending += n
		case framing == FramingConfirmed:
			c.AwaitingOpinion += n
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Decisões do departamento: vêm das rodadas, que sobrevivem à recusa (o projeto recusado sai do alcance do Revisor).
	drows, err := s.pool.Query(ctx, `SELECT decision, count(*) FROM project_framings
		WHERE department_id = $1 AND decision IN ('confirmed', 'refused')
		AND decided_at >= now() - make_interval(days => `+strconv.Itoa(archivedWindowDays)+`) GROUP BY 1`, dept)
	if err != nil {
		return err
	}
	defer drows.Close()
	for drows.Next() {
		var decision string
		var n int
		if err := drows.Scan(&decision, &n); err != nil {
			return err
		}
		switch decision {
		case FramingConfirmed:
			out.Counts.Decided30d.Confirmed = n
		case FramingRefused:
			out.Counts.Decided30d.Refused = n
		}
	}
	return drows.Err()
}

// reviewerPending preenche o bloco dos enquadramentos pendentes: os que estão há mais tempo com o departamento primeiro.
func (s *Service) reviewerPending(ctx context.Context, q *queueQuery, out *ReviewerOverview) error {
	rows, err := s.pool.Query(ctx, q.cte()+` SELECT w.id, w.title, COALESCE(w.code, ''), w.institution_name, w.late_submission, COALESCE(w.since, 0),
		f.round, f.indicated_at, COALESCE((SELECT NULLIF(btrim(a.first_name || ' ' || a.last_name), '') FROM admins a WHERE a.id = f.indicated_by_id), ''),
		count(*) OVER ()
		FROM w JOIN project_framings f ON f.project_id = w.id AND f.decision = 'pending'
		WHERE w.work = '`+workFraming+`'
		ORDER BY w.since DESC NULLS LAST, w.id LIMIT `+strconv.Itoa(reviewerBlockItems), q.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it ReviewerItem
		if err := rows.Scan(&it.ProjectID, &it.Title, &it.Code, &it.Institution, &it.Late, &it.SinceSeconds,
			&it.Round, &it.IndicatedAt, &it.IndicatedBy, &out.Pending.Total); err != nil {
			return err
		}
		it.OverLimit = out.Limits.Framing > 0 && it.SinceSeconds >= int64(out.Limits.Framing)*86400
		out.Pending.Items = append(out.Pending.Items, it)
	}
	return rows.Err()
}
