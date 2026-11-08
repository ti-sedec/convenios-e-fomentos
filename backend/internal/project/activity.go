package project

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// Tipos (kind) de ActivityItem.
const (
	ActivitySubmitted          = "submitted"
	ActivityAnalysisStarted    = "analysis_started"
	ActivityComplementRequest  = "complement_requested"
	ActivityResubmitted        = "resubmitted"
	ActivityAwaitingOpinion    = "awaiting_opinion"
	ActivityArchived           = "archived"
	ActivitySharedWithYou      = "shared_with_you"
	defaultActivityLimit       = 10
	maxActivityLimit           = 30
	actionSubmit               = "project.submit"
	actionStartAnalysis        = "project.start-analysis"
	actionTakeOver             = "project.take-over"
	actionComplementRequest    = "project.complement-request"
	actionResubmit             = "project.resubmit"
	actionFramingConfirm       = "project.framing-confirm"
	actionArchive              = "project.archive"
	actionMemberInvited        = "project.member-invited"
	activityByYou              = "you"
	activityByMember           = "member"
	activityByTeam             = "team"
	activityBySystem           = "system"
	activityAuditEntityProject = "project"
)

// activityKinds é a lista PERMITIDA do feed: ação de auditoria → tipo mostrado ao proponente. O feed vem de audit_logs, que mistura o que
// é do proponente com o que é interno da equipe (departamento, recusa de enquadramento, conferência, arquivos abertos). Ação fora desta
// lista nunca é lida. Um evento novo no sistema só aparece aqui se alguém o incluir de propósito, depois de decidir o que dele o
// proponente pode saber.
var activityKinds = map[string]string{
	actionSubmit:            ActivitySubmitted,
	actionStartAnalysis:     ActivityAnalysisStarted,
	actionComplementRequest: ActivityComplementRequest,
	actionResubmit:          ActivityResubmitted,
	actionFramingConfirm:    ActivityAwaitingOpinion,
	actionArchive:           ActivityArchived,
	actionMemberInvited:     ActivitySharedWithYou,
}

// ActivityItem é um marco na vida de um projeto, escrito para o proponente. Data só leva campos montados aqui, nunca o payload da
// auditoria. By diz quem agiu sem dar nome: you (a própria pessoa), member (titular ou outro membro), team (a equipe) ou system.
type ActivityItem struct {
	ID        uuid.UUID      `json:"id"`
	At        time.Time      `json:"at"`
	Kind      string         `json:"kind"`
	By        string         `json:"by"`
	ProjectID uuid.UUID      `json:"projectId"`
	Title     string         `json:"title"`
	Code      string         `json:"code,omitempty"`
	Data      map[string]any `json:"data"`
}

// activityData monta o Data de um evento a partir do payload da auditoria, copiando só os campos permitidos de cada tipo. Valor que não
// tem o formato esperado é descartado, e payload ilegível vira Data vazio.
func activityData(kind string, payload []byte) map[string]any {
	out := map[string]any{}
	var p map[string]any
	if json.Unmarshal(payload, &p) != nil {
		return out
	}
	switch kind {
	case ActivitySubmitted:
		if late, ok := p["late"].(bool); ok {
			out["late"] = late
		}
	case ActivityComplementRequest:
		if n, ok := p["items"].(float64); ok {
			out["items"] = int(n)
		}
		if s, ok := p["dueAt"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				out["dueAt"] = t
			}
		}
	case ActivityResubmitted:
		if n, ok := p["version"].(float64); ok {
			out["version"] = int(n)
		}
	case ActivityArchived:
		reason, _ := p["reason"].(string)
		switch reason {
		case ArchiveComplementDeadline, ArchiveWithdrawn, ArchiveDenied, ArchiveMissingDocuments:
		default:
			reason = ArchiveOther
		}
		out["reason"] = reason
	case ActivitySharedWithYou:
		if l, ok := p["level"].(string); ok && (l == LevelRead || l == LevelEdit) {
			out["level"] = l
		}
	}
	return out
}

// Activity devolve os marcos mais recentes dos projetos da pessoa (titular ou membro ativo, a mesma regra de Overview), do mais novo
// ao mais antigo. O titular vê tudo desde a criação do projeto. O membro, só o que aconteceu depois de receber o acesso, e o convite
// aparece só para ele. Quem perde o acesso deixa de ver na hora. access vazio equivale a AccessFilterAny; limit fora de 1..30 vira 10.
func (s *Service) Activity(ctx context.Context, actor Actor, perm authz.Filter, access string, limit int) ([]ActivityItem, error) {
	if access == "" {
		access = AccessFilterAny
	}
	if limit < 1 || limit > maxActivityLimit {
		limit = defaultActivityLimit
	}
	conds, args, _, err := s.scopeConds(ctx, perm, actor, access)
	if err != nil {
		return nil, err
	}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	actions := make([]string, 0, len(activityKinds))
	for a := range activityKinds {
		actions = append(actions, a)
	}
	// A pessoa vai como texto, e todas as comparações são feitas em texto: audit_logs.actor_id é texto, e um mesmo parâmetro não pode ser
	// texto num lugar e uuid em outro.
	me, actionsArg, invited := arg(actor.ID.String()), arg(actions), arg(actionMemberInvited)
	// Os projetos do escopo ficam numa subconsulta: as condições usam colunas sem prefixo, e audit_logs também tem id e created_at.
	rows, err := s.pool.Query(ctx, `SELECT l.id, l.created_at, l.action, l.payload,
		CASE WHEN l.actor_type = 'system' THEN '`+activityBySystem+`'
			WHEN l.actor_id = `+me+` THEN '`+activityByYou+`'
			WHEN l.actor_id = p.created_by_id::text OR EXISTS (SELECT 1 FROM project_members pm JOIN admins ad ON ad.cpf = pm.cpf
				WHERE pm.project_id = p.id AND ad.id::text = l.actor_id) THEN '`+activityByMember+`'
			ELSE '`+activityByTeam+`' END,
		p.id, p.title, p.code
		FROM audit_logs l
		JOIN (SELECT id, title, COALESCE(code, '') AS code, created_by_id FROM projects`+whereOf(conds)+`) p ON l.entity_id = p.id::text
		WHERE l.entity = '`+activityAuditEntityProject+`' AND l.action = ANY(`+actionsArg+`)
		AND (p.created_by_id::text = `+me+` OR l.created_at >= (SELECT pm.created_at FROM project_members pm JOIN admins ad ON ad.cpf = pm.cpf
			WHERE pm.project_id = p.id AND ad.id::text = `+me+` AND pm.status = 'active'))
		AND (l.action <> `+invited+` OR l.payload->>'member' IN (SELECT pm.id::text FROM project_members pm JOIN admins ad ON ad.cpf = pm.cpf
			WHERE pm.project_id = p.id AND ad.id::text = `+me+`))
		ORDER BY l.created_at DESC, l.id DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityItem{}
	for rows.Next() {
		var it ActivityItem
		var action string
		var payload []byte
		if err := rows.Scan(&it.ID, &it.At, &action, &payload, &it.By, &it.ProjectID, &it.Title, &it.Code); err != nil {
			return nil, err
		}
		it.Kind = activityKinds[action]
		it.Data = activityData(it.Kind, payload)
		out = append(out, it)
	}
	return out, rows.Err()
}
