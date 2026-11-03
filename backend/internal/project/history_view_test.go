package project

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

func TestBuildHistory(t *testing.T) {
	t0 := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	ptrT := func(v time.Time) *time.Time { return &v }
	admin, dept := uuid.New(), uuid.New()
	rows := []db.ListStageHistoryRow{
		{Seq: 1, Status: "submitted", Stage: "awaiting_checklist", HolderType: "team", EnteredAt: at(0), LeftAt: ptrT(at(5)), EnteredByType: "admin", Transition: "project.submit"},
		{Seq: 2, Status: "submitted", Stage: "in_analysis", HolderType: "analyst", HolderAdminID: &admin, HolderAdminName: "Ana", EnteredAt: at(5), LeftAt: ptrT(at(29)), EnteredByType: "admin", Transition: "project.start-analysis"},
		{Seq: 3, Status: "submitted", Stage: "in_analysis", HolderType: "department", HolderDepartmentID: &dept, HolderDepartmentName: ptr("Financeiro"), EnteredAt: at(29), EnteredByType: "admin", Transition: "project.assign-department"},
	}

	t.Run("trecho aberto conta até agora", func(t *testing.T) {
		h := buildHistory(rows, at(31))
		assert.Equal(t, int64(5*3600), h.Segments[0].DurationSecs)
		assert.Equal(t, int64(24*3600), h.Segments[1].DurationSecs)
		assert.Equal(t, int64(2*3600), h.Segments[2].DurationSecs)
		assert.Equal(t, "Ana", h.Segments[1].Analyst.Name)
		assert.Equal(t, "Financeiro", h.Segments[2].Department.Name)
		assert.Nil(t, h.Segments[2].LeftAt)
		assert.Equal(t, int64(31*3600), h.Summary.SinceSubmissionSecs)
		assert.Equal(t, int64(26*3600), h.Summary.CurrentStageSecs, "análise e enquadramento pendente são a mesma etapa")
		assert.Equal(t, map[string]int64{"team": 5 * 3600, "analyst": 24 * 3600, "department": 2 * 3600}, h.Summary.ByHolder)
		assert.Equal(t, map[string]int64{"awaiting_checklist": 5 * 3600, "in_analysis": 26 * 3600}, h.Summary.ByStage)
	})

	t.Run("arquivado não conta tempo", func(t *testing.T) {
		closed := append([]db.ListStageHistoryRow(nil), rows...)
		closed[2].LeftAt = ptrT(at(30))
		closed = append(closed, db.ListStageHistoryRow{Seq: 4, Status: "archived", Stage: "in_analysis", HolderType: "none", EnteredAt: at(30), EnteredByType: "system", Transition: "project.archive"})
		h := buildHistory(closed, at(500))
		assert.Equal(t, int64(30*3600), h.Summary.SinceSubmissionSecs)
		assert.Zero(t, h.Summary.CurrentStageSecs)
		assert.Zero(t, h.Segments[3].DurationSecs)
		assert.NotContains(t, h.Summary.ByHolder, "none")
		assert.Equal(t, "system", h.Segments[3].EnteredBy.Type)
	})

	t.Run("sem trechos", func(t *testing.T) {
		h := buildHistory(nil, at(1))
		assert.Empty(t, h.Segments)
		assert.NotNil(t, h.Segments)
		assert.Zero(t, h.Summary.SinceSubmissionSecs)
	})

	t.Run("trecho estimado", func(t *testing.T) {
		h := buildHistory([]db.ListStageHistoryRow{{Seq: 1, Status: "submitted", Stage: "in_analysis", HolderType: "team", EnteredAt: at(0), EnteredByType: "system", Source: "initial", Transition: "initial"}}, at(1))
		assert.True(t, h.Segments[0].Estimated)
	})
}
