package project

import (
	"context"
	"strconv"
)

// MyCounts é o que a pessoa fez numa janela de tempo.
type MyCounts struct {
	Assumed     int `json:"assumed"`     // análises que assumiu
	Complements int `json:"complements"` // pedidos de complementação que fez
	Indicated   int `json:"indicated"`   // departamentos que indicou
	Archived    int `json:"archived"`    // projetos que arquivou (não conta desistência nem arquivamento pelo sistema)
}

// MySummary é a resposta de GET /admin/projects/queue/me: os números de quem chama, nos últimos 7 e 30 dias.
type MySummary struct {
	Last7  MyCounts `json:"last7"`
	Last30 MyCounts `json:"last30"`
}

// QueueMe conta o que o próprio administrador fez, pela auditoria. Não recebe a pessoa por parâmetro: é sempre quem chama, e nenhum
// outro perfil consulta os números de alguém. Comparar pessoas seria outra decisão (painel de prazos, T29).
func (s *Service) QueueMe(ctx context.Context, actor Actor) (*MySummary, error) {
	var out MySummary
	// start-analysis e take-over só são gravados quando a análise muda de mãos (são idempotentes), então cada evento é uma análise
	// assumida de verdade (iniciada ou tomada de outro analista).
	// Arquivar com o motivo "desistência" é do proponente (e, se for a mesma pessoa, não é trabalho de Analista).
	count := func(action string, days int, extra string) string {
		cond := `action = '` + action + `'`
		if action == actionStartAnalysis {
			cond = `action IN ('` + actionStartAnalysis + `', '` + actionTakeOver + `')`
		}
		return `count(*) FILTER (WHERE ` + cond + ` AND created_at >= now() - make_interval(days => ` + strconv.Itoa(days) + `)` + extra + `)`
	}
	cols := []struct {
		dst    *int
		action string
		days   int
		extra  string
	}{
		{&out.Last7.Assumed, actionStartAnalysis, 7, ""}, {&out.Last7.Complements, actionComplementRequest, 7, ""},
		{&out.Last7.Indicated, "project.assign-department", 7, ""}, {&out.Last7.Archived, actionArchive, 7, archiveByTeam},
		{&out.Last30.Assumed, actionStartAnalysis, 30, ""}, {&out.Last30.Complements, actionComplementRequest, 30, ""},
		{&out.Last30.Indicated, "project.assign-department", 30, ""}, {&out.Last30.Archived, actionArchive, 30, archiveByTeam},
	}
	sql, dst := "SELECT ", make([]any, 0, len(cols))
	for i, c := range cols {
		if i > 0 {
			sql += ", "
		}
		sql += count(c.action, c.days, c.extra)
		dst = append(dst, c.dst)
	}
	sql += ` FROM audit_logs WHERE actor_type = 'admin' AND actor_id = $1 AND entity = 'project' AND created_at >= now() - make_interval(days => 30)`
	if err := s.pool.QueryRow(ctx, sql, actor.ID.String()).Scan(dst...); err != nil {
		return nil, err
	}
	return &out, nil
}

// archiveByTeam deixa de fora a desistência (que é do proponente).
const archiveByTeam = ` AND payload->>'reason' IS DISTINCT FROM '` + ArchiveWithdrawn + `'`
