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

func TestListaPermitidaDoFeed(t *testing.T) {
	// Ações internas da equipe nunca entram no feed do proponente.
	for _, banned := range []string{"project.assign-department", "project.framing-indicated", "project.framing-refuse", "project.framing-refused",
		"project.review-item", "project.file-download", "project.file-preview", "project.file-infected", "project.receipt-download",
		"project.update", "project.create", "project.delete", "project.duplicate", "project.draft-expired", "project.member-updated", "project.member-revoked"} {
		if _, ok := activityKinds[banned]; ok {
			t.Errorf("%s não pode estar na lista permitida do feed", banned)
		}
	}
}

func TestDadosDoFeedCopiamSoOPermitido(t *testing.T) {
	for _, x := range []struct {
		name, kind, payload string
		want                map[string]any
	}{
		{"enquadramento não leva nada", ActivityAwaitingOpinion, `{"round":2,"department":"d1f0"}`, map[string]any{}},
		{"análise não leva nada", ActivityAnalysisStarted, `{"stage":"in_analysis"}`, map[string]any{}},
		{"envio", ActivitySubmitted, `{"code":"2026-000001","late":true,"version":1}`, map[string]any{"late": true}},
		{"reenvio", ActivityResubmitted, `{"version":3}`, map[string]any{"version": 3}},
		{"arquivamento conhecido", ActivityArchived, `{"reason":"denied","note":"texto interno"}`, map[string]any{"reason": "denied"}},
		{"arquivamento de motivo desconhecido", ActivityArchived, `{"reason":"qualquer coisa"}`, map[string]any{"reason": "other"}},
		{"arquivamento sem motivo", ActivityArchived, `{}`, map[string]any{"reason": "other"}},
		{"convite", ActivitySharedWithYou, `{"member":"x","cpf":"***.123.456-**","level":"read"}`, map[string]any{"level": "read"}},
		{"nível inválido do convite", ActivitySharedWithYou, `{"level":"admin"}`, map[string]any{}},
		{"payload ilegível", ActivitySubmitted, `não é json`, map[string]any{}},
	} {
		got := activityData(x.kind, []byte(x.payload))
		if len(got) != len(x.want) {
			t.Errorf("%s: %v, esperado %v", x.name, got, x.want)
			continue
		}
		for k, v := range x.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, esperado %v", x.name, k, got[k], v)
			}
		}
	}
	got := activityData(ActivityComplementRequest, []byte(`{"number":1,"items":4,"dueAt":"2026-11-03T12:00:00Z"}`))
	if got["items"] != 4 || got["dueAt"] == nil || len(got) != 2 {
		t.Errorf("pedido de complementação: %v", got)
	}
	if got := activityData(ActivityComplementRequest, []byte(`{"dueAt":"amanhã"}`)); len(got) != 0 {
		t.Errorf("data inválida deve ser descartada: %v", got)
	}
}

func TestListaPermitidaDoFeedDaEquipe(t *testing.T) {
	// O que é só de conferência, arquivo, edição e compartilhamento nunca entra, nem no feed da equipe.
	for _, banned := range []string{"project.review-item", "project.file-download", "project.file-preview", "project.file-infected", "project.receipt-download",
		"project.update", "project.create", "project.delete", "project.duplicate", "project.draft-expired", "project.member-invited",
		"project.member-updated", "project.member-revoked", "project.declaration-request", "project.declaration-signed"} {
		if _, ok := queueActivityKinds[banned]; ok {
			t.Errorf("%s não pode estar na lista permitida do feed da equipe", banned)
		}
	}
}

func TestDadosDoFeedDaEquipeCopiamSoOPermitido(t *testing.T) {
	for _, x := range []struct {
		name, kind, payload, dept string
		want                      map[string]any
	}{
		{"indicação leva o departamento", QueueActDepartmentAssigned, `{"round":1,"department":"d1f0"}`, "Financeiro", map[string]any{"department": "Financeiro"}},
		{"recusa leva só o departamento", QueueActFramingRefused, `{"round":2,"department":"d1f0","justification":"texto interno"}`, "Jurídico", map[string]any{"department": "Jurídico"}},
		{"confirmação", QueueActFramingConfirmed, `{"round":1}`, "Financeiro", map[string]any{"department": "Financeiro"}},
		{"sem departamento", QueueActFramingConfirmed, `{}`, "", map[string]any{}},
		{"análise não leva nada", QueueActAnalysisStarted, `{"stage":"in_analysis"}`, "", map[string]any{}},
		{"envio", QueueActSubmitted, `{"code":"2026-000001","late":true,"version":1}`, "", map[string]any{"late": true}},
		{"reenvio", QueueActResubmitted, `{"version":3}`, "", map[string]any{"version": 3}},
		{"arquivamento conhecido", QueueActArchived, `{"reason":"denied","note":"texto interno"}`, "", map[string]any{"reason": "denied"}},
		{"arquivamento de motivo desconhecido", QueueActArchived, `{"reason":"qualquer coisa"}`, "", map[string]any{"reason": "other"}},
		{"payload ilegível", QueueActSubmitted, `não é json`, "", map[string]any{}},
	} {
		got := queueActivityData(x.kind, []byte(x.payload), x.dept)
		if len(got) != len(x.want) {
			t.Errorf("%s: %v, esperado %v", x.name, got, x.want)
			continue
		}
		for k, v := range x.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, esperado %v", x.name, k, got[k], v)
			}
		}
	}
	got := queueActivityData(QueueActComplementRequest, []byte(`{"number":1,"items":4,"dueAt":"2026-11-03T12:00:00Z"}`), "")
	if got["items"] != 4 || got["dueAt"] == nil || len(got) != 2 {
		t.Errorf("pedido de complementação: %v", got)
	}
}
