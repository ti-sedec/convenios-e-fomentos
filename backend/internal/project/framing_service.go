package project

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

const (
	minFramingNote = 3
	maxFramingNote = 1000
)

// DepartmentRef é o departamento de uma rodada. O id some se o departamento for excluído; o nome é a cópia da data da indicação.
type DepartmentRef struct {
	ID   *uuid.UUID `json:"id"`
	Name string     `json:"name"`
}

// Framing é uma rodada de enquadramento: o Analista indicou um departamento e o Revisor dele decidiu (ou ainda não).
type Framing struct {
	Round         int           `json:"round"`
	Department    DepartmentRef `json:"department"`
	Decision      string        `json:"decision"` // pending | confirmed | refused
	IndicatedBy   string        `json:"indicatedBy"`
	IndicatedAt   time.Time     `json:"indicatedAt"`
	DecidedBy     string        `json:"decidedBy,omitempty"`
	DecidedAt     *time.Time    `json:"decidedAt,omitempty"`
	Justification string        `json:"justification,omitempty"`
}

// DepartmentInput é o corpo de PUT /department.
type DepartmentInput struct {
	DepartmentID uuid.UUID `json:"departmentId"`
}

// FramingDecisionInput é o corpo de confirmar e recusar. Round é a rodada que o Revisor viu: evita decidir sobre uma rodada antiga.
type FramingDecisionInput struct {
	Round         int32  `json:"round"`
	Justification string `json:"justification"`
}

func framingPending(r db.Project) bool {
	return r.FramingStatus != nil && *r.FramingStatus == FramingPending
}

func errFramingPending() error {
	return httpapi.ErrConflict("framing_pending", "Há um enquadramento aguardando a decisão do Revisor do Departamento.")
}

// errStage explica por que a ação não cabe na etapa atual do projeto.
func errStage(stage string) error {
	switch stage {
	case StageAwaitingProponent:
		return httpapi.ErrConflict("project_stage_invalid", "O projeto está aguardando o proponente.")
	case StageAwaitingOpinion:
		return httpapi.ErrConflict("project_stage_invalid", "O enquadramento já foi confirmado; o projeto aguarda o parecer.")
	default:
		return httpapi.ErrConflict("project_stage_invalid", "O projeto não está em uma etapa que permita esta ação.")
	}
}

func cleanJustification(text string) (string, error) {
	text = multiline(text)
	if n := len([]rune(text)); n < minFramingNote || n > maxFramingNote {
		return "", fieldIssue("justification", "Informe a justificativa, de 3 a 1.000 caracteres.")
	}
	return text, nil
}

// AssignDepartment indica o departamento de enquadramento: abre uma rodada que o Revisor do Departamento vai decidir. Exige o projeto
// em análise com TUDO aprovado (recalculado aqui, sob a trava da linha), um departamento finalístico e ao menos um Revisor ativo nele.
func (s *Service) AssignDepartment(ctx context.Context, actor Actor, id uuid.UUID, in DepartmentInput, dec authz.Decision) (*Project, error) {
	var out *Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := s.lockedForReview(ctx, q, id, dec)
		if err != nil {
			return err
		}
		switch str(cur.Stage) {
		case StageInAnalysis:
		case StageAwaitingChecklist:
			return errStageForReview(StageAwaitingChecklist)
		default:
			return errStage(str(cur.Stage))
		}
		if framingPending(cur) {
			return errFramingPending()
		}
		_, cl, err := s.viewWithChecklist(ctx, q, cur, actor)
		if err != nil {
			return err
		}
		rv, err := s.reviewState(ctx, q, id, cl, nil)
		if err != nil {
			return err
		}
		if !rv.AllApproved {
			e := httpapi.ErrConflict("review_incomplete", "Aprove todos os itens da conferência antes de indicar o departamento.")
			e.Details = map[string]any{"pending": rv.Summary.Pending, "rejected": rv.Summary.Rejected, "blockers": rv.Summary.Blockers}
			return e
		}
		d, err := q.GetDepartmentForShare(ctx, in.DepartmentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fieldIssue("departmentId", "Departamento inexistente.")
		}
		if err != nil {
			return err
		}
		if !d.IsFinalistic {
			return &httpapi.APIError{Status: 422, Code: "department_not_finalistic", Message: "Este departamento não é área finalística e não recebe projetos para enquadramento."}
		}
		n, err := q.CountActiveReviewers(ctx, &d.ID)
		if err != nil {
			return err
		}
		if n == 0 {
			return &httpapi.APIError{Status: 422, Code: "department_without_reviewer", Message: "Este departamento não tem nenhum Revisor ativo para decidir o enquadramento."}
		}
		round, err := q.NextFramingRound(ctx, id)
		if err != nil {
			return err
		}
		if _, err := q.InsertFraming(ctx, db.InsertFramingParams{ProjectID: id, Round: round, DepartmentID: &d.ID, DepartmentName: d.Name, IndicatedByID: &actor.ID}); err != nil {
			return err
		}
		row, err := q.SetProjectFraming(ctx, db.SetProjectFramingParams{ID: id, DepartmentID: &d.ID, FramingStatus: ptr(FramingPending), Stage: ptr(StageInAnalysis), UpdatedByID: &actor.ID})
		if err != nil {
			return notFound(err)
		}
		if err := s.transition(ctx, q, row, actor.entry("project.assign-department", id, map[string]any{"department": d.ID, "round": round})); err != nil {
			return err
		}
		if out, err = s.view(ctx, q, row, actor); err != nil {
			return err
		}
		return s.notifyFramingIndicated(ctx, q, out, int(round), d.Name)
	})
	return out, err
}

// ConfirmFraming é o "Sim" do Revisor do Departamento: o projeto se enquadra no departamento e passa a aguardar o parecer.
func (s *Service) ConfirmFraming(ctx context.Context, actor Actor, id uuid.UUID, in FramingDecisionInput, dec authz.Decision) (*Project, error) {
	return s.decideFraming(ctx, actor, id, in, dec, true)
}

// RefuseFraming é o "Não": o departamento é limpo, a rodada fica registrada com a justificativa e o projeto volta ao Analista, que
// pode indicar outro departamento (a conferência continua toda aprovada). Não há limite de recusas.
func (s *Service) RefuseFraming(ctx context.Context, actor Actor, id uuid.UUID, in FramingDecisionInput, dec authz.Decision) (*Project, error) {
	return s.decideFraming(ctx, actor, id, in, dec, false)
}

func (s *Service) decideFraming(ctx context.Context, actor Actor, id uuid.UUID, in FramingDecisionInput, dec authz.Decision, confirm bool) (*Project, error) {
	justification := ""
	if !confirm {
		var err error
		if justification, err = cleanJustification(in.Justification); err != nil {
			return nil, err
		}
	}
	var out *Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		// Se outro Revisor acabou de recusar, o projeto já não tem departamento; a permissão vale então contra o departamento da
		// última rodada, e quem chegou depois recebe "já decidido" (409) em vez de um 403 confuso.
		ref := refOf(cur)
		latest, lerr := q.GetLatestFraming(ctx, id)
		if lerr != nil && !errors.Is(lerr, pgx.ErrNoRows) {
			return lerr
		}
		if ref.DepartmentID == nil && lerr == nil {
			ref.DepartmentID = latest.DepartmentID
		}
		if !dec.AllowsRecord(ref.Getter()) {
			return httpapi.ErrForbidden()
		}
		if cur.Status != StatusSubmitted {
			return httpapi.ErrConflict("project_not_submitted", "Só projetos enviados e ativos têm enquadramento.")
		}
		pending, err := q.GetPendingFraming(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.ErrConflict("framing_already_decided", "O enquadramento deste projeto já foi decidido.")
		}
		if err != nil {
			return err
		}
		if pending.Round != in.Round {
			return httpapi.ErrConflict("framing_round_stale", "A indicação de departamento mudou desde que você abriu o projeto. Recarregue para ver a rodada atual.")
		}

		decision, status, stage, department := FramingConfirmed, ptr(FramingConfirmed), StageAwaitingOpinion, cur.DepartmentID
		if !confirm {
			decision, status, stage, department = FramingRefused, nil, StageInAnalysis, nil
		}
		if _, err := q.DecideFraming(ctx, db.DecideFramingParams{ID: pending.ID, Decision: decision, DecidedByID: &actor.ID, Justification: justification}); err != nil {
			return notFound(err)
		}
		row, err := q.SetProjectFraming(ctx, db.SetProjectFramingParams{ID: id, DepartmentID: department, FramingStatus: status, Stage: ptr(stage), UpdatedByID: &actor.ID})
		if err != nil {
			return notFound(err)
		}
		action := "project.framing-confirm"
		if !confirm {
			action = "project.framing-refuse"
		}
		if err := s.transition(ctx, q, row, actor.entry(action, id, map[string]any{"round": pending.Round, "department": pending.DepartmentID})); err != nil {
			return err
		}
		if out, err = s.view(ctx, q, row, actor); err != nil {
			return err
		}
		if !confirm {
			return s.notifyFramingRefused(ctx, q, out, int(pending.Round), pending.DepartmentName, justification)
		}
		return nil
	})
	return out, err
}

// framingViews monta o histórico de rodadas, da mais antiga à mais nova, e a rodada em curso (a última, se o projeto tem
// enquadramento pendente ou confirmado).
func (s *Service) framingViews(ctx context.Context, q *db.Queries, r db.Project) (current *Framing, all []Framing, err error) {
	rows, err := q.ListFramings(ctx, r.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, x := range rows {
		all = append(all, Framing{
			Round: int(x.Round), Department: DepartmentRef{ID: x.DepartmentID, Name: x.DepartmentName}, Decision: x.Decision,
			IndicatedBy: x.IndicatedByName, IndicatedAt: x.IndicatedAt, DecidedBy: strings.TrimSpace(x.DecidedByName), DecidedAt: x.DecidedAt,
			Justification: x.Justification,
		})
	}
	if r.FramingStatus != nil && len(all) > 0 {
		last := all[len(all)-1]
		current = &last
	}
	return current, all, nil
}
