package project

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// Tipos (kind) de QueueActivityItem.
const (
	QueueActSubmitted          = "submitted"
	QueueActResubmitted        = "resubmitted"
	QueueActAnalysisStarted    = "analysis_started"
	QueueActComplementRequest  = "complement_requested"
	QueueActDepartmentAssigned = "department_assigned"
	QueueActFramingConfirmed   = "framing_confirmed"
	QueueActFramingRefused     = "framing_refused"
	QueueActArchived           = "archived"

	defaultQueueActivityLimit = 15
	maxQueueActivityLimit     = 30
)

// queueActivityKinds é a lista PERMITIDA do feed da equipe: ação de auditoria → tipo mostrado. É própria, separada da do proponente
// (activityKinds): a equipe vê o que é interno (departamento, recusa do enquadramento), e o que um perfil pode ver não pode ampliar o do
// outro. Fora dela: review-item, file-*, receipt-download, update, create, duplicate, delete, member-* e as declarações. Ação nova só
// entra aqui de propósito.
var queueActivityKinds = map[string]string{
	actionSubmit:                QueueActSubmitted,
	actionResubmit:              QueueActResubmitted,
	actionStartAnalysis:         QueueActAnalysisStarted,
	actionComplementRequest:     QueueActComplementRequest,
	"project.assign-department": QueueActDepartmentAssigned,
	actionFramingConfirm:        QueueActFramingConfirmed,
	"project.framing-refuse":    QueueActFramingRefused,
	actionArchive:               QueueActArchived,
}

// QueueActor é quem agiu. O proponente (titular ou membro do projeto) aparece só como tipo, sem nome nem CPF; a equipe, com o nome.
type QueueActor struct {
	Type string `json:"type"` // admin (equipe), proponent ou system
	Name string `json:"name,omitempty"`
	// Self: foi a própria pessoa que consulta (a tela escreve "Você").
	Self bool `json:"self,omitempty"`
}

// QueueActivityItem é um marco na vida de um projeto, escrito para a equipe. Data só leva campos montados aqui, nunca o payload da
// auditoria; a justificativa da recusa do enquadramento fica no detalhe do projeto, não no feed.
type QueueActivityItem struct {
	ID        uuid.UUID      `json:"id"`
	At        time.Time      `json:"at"`
	Kind      string         `json:"kind"`
	By        QueueActor     `json:"by"`
	ProjectID uuid.UUID      `json:"projectId"`
	Title     string         `json:"title"`
	Code      string         `json:"code,omitempty"`
	Data      map[string]any `json:"data"`
}

// queueActivityData monta o Data de um evento: copia só os campos permitidos de cada tipo. department é o nome do departamento da rodada
// de enquadramento do evento (vazio se não se aplica).
func queueActivityData(kind string, payload []byte, department string) map[string]any {
	out := map[string]any{}
	var p map[string]any
	if json.Unmarshal(payload, &p) != nil {
		p = map[string]any{}
	}
	switch kind {
	case QueueActSubmitted:
		if late, ok := p["late"].(bool); ok {
			out["late"] = late
		}
	case QueueActResubmitted:
		if n, ok := p["version"].(float64); ok {
			out["version"] = int(n)
		}
	case QueueActComplementRequest:
		if n, ok := p["items"].(float64); ok {
			out["items"] = int(n)
		}
		if s, ok := p["dueAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				out["dueAt"] = t
			}
		}
	case QueueActDepartmentAssigned, QueueActFramingConfirmed, QueueActFramingRefused:
		if department != "" {
			out["department"] = department
		}
	case QueueActArchived:
		reason, _ := p["reason"].(string)
		switch reason {
		case ArchiveComplementDeadline, ArchiveWithdrawn, ArchiveDenied, ArchiveMissingDocuments:
		default:
			reason = ArchiveOther
		}
		out["reason"] = reason
	}
	return out
}

// QueueActivity devolve os marcos mais recentes dos projetos que o Analista enxerga (a permissão da ação somada a projeto enviado ou
// arquivado), do mais novo ao mais antigo. scope=mine restringe aos que a pessoa assumiu. limit fora de 1..30 vira 15.
func (s *Service) QueueActivity(ctx context.Context, actor Actor, perm authz.Filter, scope string, limit int) ([]QueueActivityItem, error) {
	if limit < 1 || limit > maxQueueActivityLimit {
		limit = defaultQueueActivityLimit
	}
	conds, args, _, err := s.scopeConds(ctx, authz.And(perm, submittedFilter()), actor, "")
	if err != nil {
		return nil, err
	}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	if scope == QueueScopeMine {
		conds = append(conds, "analyst_id = "+arg(actor.ID))
	}
	actions := make([]string, 0, len(queueActivityKinds))
	for a := range queueActivityKinds {
		actions = append(actions, a)
	}
	me, actionsArg := arg(actor.ID.String()), arg(actions)
	// Os projetos do escopo ficam numa subconsulta: as condições usam colunas sem prefixo, e audit_logs também tem id e created_at.
	rows, err := s.pool.Query(ctx, `SELECT l.id, l.created_at, l.action, l.payload,
		CASE WHEN l.actor_type = 'system' THEN 'system'
			WHEN l.actor_id = p.created_by_id::text OR EXISTS (SELECT 1 FROM project_members pm JOIN admins pa ON pa.cpf = pm.cpf
				WHERE pm.project_id = p.id AND pa.id::text = l.actor_id) THEN 'proponent'
			ELSE 'admin' END,
		COALESCE(l.actor_id = `+me+`, false),
		COALESCE(NULLIF(btrim(ad.first_name || ' ' || ad.last_name), ''), ad.email, ''),
		COALESCE((SELECT f.department_name FROM project_framings f WHERE f.project_id = p.id
			AND f.round = CASE WHEN l.payload->>'round' ~ '^[0-9]+$' THEN (l.payload->>'round')::int END), ''),
		p.id, p.title, p.code
		FROM audit_logs l
		JOIN (SELECT id, title, COALESCE(code, '') AS code, created_by_id FROM projects`+whereOf(conds)+`) p ON l.entity_id = p.id::text
		LEFT JOIN admins ad ON ad.id::text = l.actor_id AND l.actor_type = 'admin'
		WHERE l.entity = 'project' AND l.action = ANY(`+actionsArg+`)
		ORDER BY l.created_at DESC, l.id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueueActivityItem{}
	for rows.Next() {
		var it QueueActivityItem
		var action, name, dept string
		var payload []byte
		if err := rows.Scan(&it.ID, &it.At, &action, &payload, &it.By.Type, &it.By.Self, &name, &dept, &it.ProjectID, &it.Title, &it.Code); err != nil {
			return nil, err
		}
		if it.By.Type == "admin" {
			it.By.Name = name
		}
		if it.By.Type != "admin" {
			it.By.Self = false
		}
		it.Kind = queueActivityKinds[action]
		it.Data = queueActivityData(it.Kind, payload, dept)
		out = append(out, it)
	}
	return out, rows.Err()
}
