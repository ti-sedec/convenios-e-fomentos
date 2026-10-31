package project

import "testing"

func TestContagemDaVisaoGeral(t *testing.T) {
	var c OverviewCounts
	for _, x := range []struct {
		status, stage string
		n             int
	}{
		{StatusDraft, "", 2},
		{StatusSubmitted, StageAwaitingChecklist, 1},
		{StatusSubmitted, StageInAnalysis, 3}, // inclui o enquadramento pendente: a etapa continua "em análise"
		{StatusSubmitted, StageAwaitingProponent, 4},
		{StatusSubmitted, StageAwaitingOpinion, 5},
		{StatusArchived, StageInAnalysis, 6}, // arquivado guarda a etapa em que estava, mas só conta como arquivado
	} {
		c.add(x.status, x.stage, x.n)
	}
	want := OverviewCounts{Draft: 2, AwaitingChecklist: 1, InAnalysis: 3, AwaitingProponent: 4, AwaitingOpinion: 5, Archived: 6}
	if c != want {
		t.Fatalf("contagens = %+v, esperado %+v", c, want)
	}
}
