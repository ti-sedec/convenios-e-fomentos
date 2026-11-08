package project

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// ReviewerActIndicated é o tipo (kind) "o Analista indicou o projeto para o departamento". Os outros tipos do feed do Revisor reaproveitam
// os nomes do feed da equipe: framing_confirmed, framing_refused e archived.
const ReviewerActIndicated = "framing_indicated"

// ReviewerActivityItem é um marco do departamento, escrito para o Revisor. Data só leva campos montados aqui, nunca o payload da auditoria;
// a justificativa da recusa não vai no feed.
type ReviewerActivityItem struct {
	// ID é texto: uma rodada gera dois marcos (a indicação e a decisão).
	ID        string     `json:"id"`
	At        time.Time  `json:"at"`
	Kind      string     `json:"kind"`
	By        QueueActor `json:"by"`
	ProjectID uuid.UUID  `json:"projectId"`
	Title     string     `json:"title"`
	Code      string     `json:"code,omitempty"`
	// Linkable: o Revisor ainda pode abrir o projeto. Depois da recusa o projeto sai do departamento e do alcance dele, e o link daria 403.
	Linkable bool           `json:"linkable"`
	Data     map[string]any `json:"data"`
}

// reviewerActivityData monta o Data de um marco: copia só os campos permitidos de cada tipo. round é a rodada do enquadramento (0 se não
// se aplica).
func reviewerActivityData(kind string, payload []byte, round int) map[string]any {
	switch kind {
	case QueueActArchived:
		return queueActivityData(kind, payload, "")
	case ReviewerActIndicated, QueueActFramingConfirmed, QueueActFramingRefused:
		return map[string]any{"round": round}
	}
	return map[string]any{}
}

// ReviewerActivity devolve os marcos mais recentes do departamento da pessoa, do mais novo ao mais antigo: indicações, confirmações e
// recusas (das rodadas de enquadramento, que sobrevivem à recusa) e os arquivamentos dos projetos do departamento (pela auditoria).
// limit fora de 1..30 vira 15. Sem departamento no cadastro, a lista vem vazia.
func (s *Service) ReviewerActivity(ctx context.Context, actor Actor, perm authz.Filter, limit int) ([]ReviewerActivityItem, error) {
	if limit < 1 || limit > maxQueueActivityLimit {
		limit = defaultQueueActivityLimit
	}
	out := []ReviewerActivityItem{}
	dept, err := s.reviewerDepartment(ctx, actor)
	if err != nil || dept == nil {
		return out, err
	}
	conds, args, _, err := s.scopeConds(ctx, authz.And(perm, submittedFilter()), actor, "")
	if err != nil {
		return nil, err
	}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	d := arg(dept.ID)
	conds = append(conds, "department_id = "+d)
	me := arg(actor.ID.String())

	// Os arquivamentos usam os projetos do alcance (numa subconsulta: as condições usam colunas sem prefixo, e audit_logs também tem id).
	// As rodadas não: o projeto recusado sai do alcance, mas o marco continua sendo do departamento.
	rows, err := s.pool.Query(ctx, `WITH ev AS (
			SELECT f.id::text || ':indicated' AS id, f.indicated_at AS at, '`+ReviewerActIndicated+`' AS kind, 'admin' AS actor_type,
				f.indicated_by_id::text AS actor_id, '{}'::jsonb AS payload, f.round, f.project_id
			FROM project_framings f WHERE f.department_id = `+d+`
			UNION ALL
			SELECT f.id::text || ':decided', f.decided_at, 'framing_' || f.decision, 'admin', f.decided_by_id::text, '{}'::jsonb, f.round, f.project_id
			FROM project_framings f WHERE f.department_id = `+d+` AND f.decision IN ('`+FramingConfirmed+`', '`+FramingRefused+`')
			UNION ALL
			SELECT l.id::text, l.created_at, '`+QueueActArchived+`', l.actor_type, l.actor_id, l.payload, 0, p.id
			FROM audit_logs l JOIN (SELECT id FROM projects`+whereOf(conds)+`) p ON l.entity_id = p.id::text
			WHERE l.entity = 'project' AND l.action = '`+actionArchive+`'
		)
		SELECT ev.id, ev.at, ev.kind,
			CASE WHEN ev.kind <> '`+QueueActArchived+`' THEN 'admin'
				WHEN ev.actor_type = 'system' THEN 'system'
				WHEN ev.actor_id = pr.created_by_id::text OR EXISTS (SELECT 1 FROM project_members pm JOIN admins pa ON pa.cpf = pm.cpf
					WHERE pm.project_id = pr.id AND pa.id::text = ev.actor_id) THEN 'proponent'
				ELSE 'admin' END,
			COALESCE(ev.actor_id = `+me+`, false),
			COALESCE(NULLIF(btrim(ad.first_name || ' ' || ad.last_name), ''), ''),
			ev.payload, ev.round, pr.id, pr.title, COALESCE(pr.code, ''),
			pr.department_id = `+d+` AND pr.status IN ('`+StatusSubmitted+`', '`+StatusArchived+`')
		FROM ev JOIN projects pr ON pr.id = ev.project_id
		LEFT JOIN admins ad ON ad.id::text = ev.actor_id AND ev.actor_type = 'admin'
		ORDER BY ev.at DESC, ev.id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it ReviewerActivityItem
		var payload []byte
		var round int
		var linkable *bool
		if err := rows.Scan(&it.ID, &it.At, &it.Kind, &it.By.Type, &it.By.Self, &it.By.Name, &payload, &round,
			&it.ProjectID, &it.Title, &it.Code, &linkable); err != nil {
			return nil, err
		}
		it.Linkable = linkable != nil && *linkable
		if it.By.Type != "admin" {
			it.By.Name, it.By.Self = "", false
		}
		it.Data = reviewerActivityData(it.Kind, payload, round)
		out = append(out, it)
	}
	return out, rows.Err()
}
