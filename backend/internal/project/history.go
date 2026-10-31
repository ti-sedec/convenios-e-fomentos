package project

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// Com quem o projeto está (project_stage_history.holder_type).
const (
	HolderTeam       = "team"       // enviado e ainda sem analista responsável
	HolderAnalyst    = "analyst"    // em análise, com analista responsável
	HolderDepartment = "department" // enquadramento pendente ou aguardando parecer
	HolderProponent  = "proponent"  // devolvido para complementação
	HolderNone       = "none"       // arquivado
)

// holderOf diz com quem o projeto está. É a única definição dessa regra: o histórico de etapas a usa ao abrir um trecho, e a
// migração 00031 repete a mesma tabela em SQL para os projetos que já existiam.
func holderOf(r db.Project) string {
	switch {
	case r.Status == StatusArchived:
		return HolderNone
	case str(r.Stage) == StageAwaitingProponent:
		return HolderProponent
	case str(r.Stage) == StageAwaitingOpinion || framingPending(r):
		return HolderDepartment
	case str(r.Stage) == StageInAnalysis && r.AnalystID != nil:
		return HolderAnalyst
	}
	return HolderTeam
}

// sameSegment diz se o trecho aberto já descreve o estado do projeto. O estado é o conjunto (situação, etapa, enquadramento,
// departamento, analista): mudar qualquer um abre um trecho novo.
func sameSegment(h db.ProjectStageHistory, r db.Project) bool {
	return h.Status == r.Status && h.Stage == str(r.Stage) && str(h.FramingStatus) == str(r.FramingStatus) &&
		sameID(h.HolderDepartmentID, holderDepartment(r)) && sameID(h.HolderAdminID, holderAdmin(r))
}

func holderDepartment(r db.Project) *uuid.UUID {
	if holderOf(r) == HolderDepartment {
		return r.DepartmentID
	}
	return nil
}

func holderAdmin(r db.Project) *uuid.UUID {
	if holderOf(r) == HolderAnalyst {
		return r.AnalystID
	}
	return nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// transition grava o evento de auditoria de uma ação que pode ter mudado o estado do projeto e acompanha o histórico de etapas:
// se o estado de row difere do trecho aberto, fecha este e abre um novo, no mesmo instante e na mesma transação. Se nada mudou
// (por exemplo, o analista já responsável abre o projeto de novo), só o evento é gravado.
//
// Toda ação que altera situação, etapa, enquadramento, departamento ou analista de um projeto enviado deve passar por aqui, depois
// de gravar a alteração. Quem chama já travou a linha do projeto (o que também ordena os trechos).
func (s *Service) transition(ctx context.Context, q *db.Queries, row db.Project, e audit.Entry) error {
	auditID, err := s.audit.RecordID(ctx, q, e)
	if err != nil {
		return err
	}
	open, err := q.GetOpenStageHistory(ctx, row.ID)
	switch {
	case err == nil:
		if sameSegment(open, row) {
			return nil
		}
		if err := q.CloseStageHistory(ctx, open.ID); err != nil {
			return err
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	var by *uuid.UUID
	if id, err := uuid.Parse(e.ActorID); err == nil {
		by = &id
	}
	_, err = q.InsertStageHistory(ctx, db.InsertStageHistoryParams{
		ProjectID: row.ID, Status: row.Status, Stage: str(row.Stage), FramingStatus: row.FramingStatus,
		HolderType: holderOf(row), HolderAdminID: holderAdmin(row), HolderDepartmentID: holderDepartment(row),
		EnteredByType: e.ActorType, EnteredByID: by, Transition: e.Action, AuditLogID: &auditID,
	})
	return err
}
