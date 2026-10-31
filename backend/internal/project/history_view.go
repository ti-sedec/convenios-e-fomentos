package project

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// HistorySegment é um trecho do histórico de etapas: o período em que o projeto esteve numa combinação estável de situação, etapa,
// enquadramento, departamento e analista.
type HistorySegment struct {
	Seq    int    `json:"seq"`
	Status string `json:"status"`
	Stage  string `json:"stage"`
	// Holder diz com quem o projeto estava: team, analyst, department, proponent ou none (arquivado).
	Holder        string        `json:"holder"`
	FramingStatus string        `json:"framingStatus,omitempty"`
	Analyst       *AnalystInfo  `json:"analyst,omitempty"`
	Department    *HistoryDept  `json:"department,omitempty"`
	EnteredAt     time.Time     `json:"enteredAt"`
	LeftAt        *time.Time    `json:"leftAt"`
	DurationSecs  int64         `json:"durationSeconds"`
	EnteredBy     *HistoryActor `json:"enteredBy,omitempty"`
	Transition    string        `json:"transition"`
	AuditLogID    *uuid.UUID    `json:"auditLogId,omitempty"`
	// Estimated marca o trecho criado pela migração para um projeto que já existia: a entrada é aproximada.
	Estimated bool `json:"estimated"`
}

type HistoryDept struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type HistoryActor struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// HistorySummary reúne os totais, em segundos corridos. Depois do arquivamento o tempo para de contar.
type HistorySummary struct {
	// SinceSubmissionSecs vai do envio até agora, ou até o arquivamento.
	SinceSubmissionSecs int64 `json:"sinceSubmissionSeconds"`
	// CurrentStageSecs soma os trechos seguidos que têm a etapa atual (o enquadramento pendente continua na etapa de análise).
	CurrentStageSecs int64 `json:"currentStageSeconds"`
	// ByHolder e ByStage somam o tempo por quem estava com o projeto e por etapa.
	ByHolder map[string]int64 `json:"byHolder"`
	ByStage  map[string]int64 `json:"byStage"`
}

// StageHistory é a resposta de GET /admin/projects/{id}/history.
type StageHistory struct {
	Segments []HistorySegment `json:"segments"`
	Summary  HistorySummary   `json:"summary"`
}

// History devolve o histórico de etapas. É interno da equipe (traz analista e departamento): o titular e os membros recebem 404,
// como nos demais campos internos do projeto. Rascunho não tem histórico.
func (s *Service) History(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) (*StageHistory, error) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) {
		return nil, httpapi.ErrNotFound()
	}
	access, err := s.accessOf(ctx, s.q, r.ID, r.CreatedByID, actor)
	if err != nil {
		return nil, err
	}
	if access != "" {
		return nil, httpapi.ErrNotFound()
	}
	rows, err := s.q.ListStageHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	return buildHistory(rows, s.now()), nil
}

// buildHistory monta a resposta e os totais. É função pura para ser testada sem banco.
func buildHistory(rows []db.ListStageHistoryRow, now time.Time) *StageHistory {
	out := &StageHistory{Segments: make([]HistorySegment, len(rows)),
		Summary: HistorySummary{ByHolder: map[string]int64{}, ByStage: map[string]int64{}}}
	for i, h := range rows {
		seg := HistorySegment{
			Seq: int(h.Seq), Status: h.Status, Stage: h.Stage, Holder: h.HolderType, FramingStatus: str(h.FramingStatus),
			EnteredAt: h.EnteredAt, LeftAt: h.LeftAt, Transition: h.Transition, AuditLogID: h.AuditLogID, Estimated: h.Source == "initial",
		}
		if h.HolderAdminID != nil {
			seg.Analyst = &AnalystInfo{ID: *h.HolderAdminID, Name: h.HolderAdminName}
		}
		if h.HolderDepartmentID != nil {
			seg.Department = &HistoryDept{ID: *h.HolderDepartmentID, Name: str(h.HolderDepartmentName)}
		}
		if h.EnteredByType != "system" || h.EnteredByID != nil {
			seg.EnteredBy = &HistoryActor{Type: h.EnteredByType, Name: h.EnteredByName}
		} else {
			seg.EnteredBy = &HistoryActor{Type: h.EnteredByType}
		}
		// Arquivado é um ponto final: o tempo não corre.
		if h.HolderType != HolderNone {
			end := now
			if h.LeftAt != nil {
				end = *h.LeftAt
			}
			seg.DurationSecs = max(int64(end.Sub(h.EnteredAt).Seconds()), 0)
			out.Summary.ByHolder[h.HolderType] += seg.DurationSecs
			out.Summary.ByStage[h.Stage] += seg.DurationSecs
			out.Summary.SinceSubmissionSecs += seg.DurationSecs
		}
		out.Segments[i] = seg
	}
	if n := len(rows); n > 0 && rows[n-1].HolderType != HolderNone {
		current := rows[n-1].Stage
		for i := n - 1; i >= 0 && rows[i].Stage == current; i-- {
			out.Summary.CurrentStageSecs += out.Segments[i].DurationSecs
		}
	}
	return out
}
