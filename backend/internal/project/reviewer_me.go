package project

import "context"

// ReviewerMyCounts é o que a pessoa decidiu numa janela de tempo.
type ReviewerMyCounts struct {
	Confirmed int `json:"confirmed"`
	Refused   int `json:"refused"`
}

// ReviewerMySummary é a resposta de GET /admin/projects/department/me: os números de quem chama, nos últimos 7 e 30 dias.
type ReviewerMySummary struct {
	Last7  ReviewerMyCounts `json:"last7"`
	Last30 ReviewerMyCounts `json:"last30"`
}

// ReviewerMe conta os enquadramentos que a própria pessoa decidiu, pelas rodadas (decided_by_id), que guardam também as recusas. Não
// recebe a pessoa por parâmetro: é sempre quem chama, e nenhum outro perfil consulta os números de alguém.
func (s *Service) ReviewerMe(ctx context.Context, actor Actor) (*ReviewerMySummary, error) {
	var out ReviewerMySummary
	err := s.pool.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE decision = '`+FramingConfirmed+`' AND decided_at >= now() - make_interval(days => 7)),
			count(*) FILTER (WHERE decision = '`+FramingRefused+`' AND decided_at >= now() - make_interval(days => 7)),
			count(*) FILTER (WHERE decision = '`+FramingConfirmed+`'),
			count(*) FILTER (WHERE decision = '`+FramingRefused+`')
		FROM project_framings WHERE decided_by_id = $1 AND decided_at >= now() - make_interval(days => 30)`, actor.ID).
		Scan(&out.Last7.Confirmed, &out.Last7.Refused, &out.Last30.Confirmed, &out.Last30.Refused)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
