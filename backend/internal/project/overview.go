package project

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// Tipos de item de Overview.Attention.
const (
	AttentionComplementOpen  = "complement_open"
	AttentionSignaturePend   = "signature_pending"
	AttentionDraft           = "draft"
	AttentionSharedNew       = "shared_new"
	maxAttention             = 20
	maxDraftsWithIssueCount  = 5
	maxPendingInvitesInQuery = 100
)

// OverviewCounts conta os projetos da pessoa por situação e etapa. Cada projeto cai em uma só contagem; o agrupamento nos cartões da
// tela é do frontend. Um projeto arquivado conta só em Archived, qualquer que fosse a etapa em que estava.
type OverviewCounts struct {
	Draft             int `json:"draft"`
	AwaitingChecklist int `json:"awaiting_checklist"`
	InAnalysis        int `json:"in_analysis"`
	AwaitingProponent int `json:"awaiting_proponent"`
	AwaitingOpinion   int `json:"awaiting_opinion"`
	Archived          int `json:"archived"`
}

// OverviewByAccess são os totais por tipo de acesso, sem o filtro de acesso pedido (alimentam o seletor "Meus / Compartilhados").
type OverviewByAccess struct {
	Owner  int `json:"owner"`
	Member int `json:"member"`
}

// OverviewCents é o valor dos projetos enviados e ainda ativos (sem rascunhos nem arquivados), no total e por instrumento.
type OverviewCents struct {
	Total    int64 `json:"total"`
	Convenio int64 `json:"convenio"`
	Fomento  int64 `json:"fomento"`
}

// AttentionItem é algo que pede uma ação do proponente. Os campos usados dependem de Kind:
// complement_open (DueAt, Overdue, Items), signature_pending (Declaration), draft (UpdatedAt, Blocking) e shared_new (Level, InvitedBy).
type AttentionItem struct {
	Kind        string     `json:"kind"`
	ProjectID   uuid.UUID  `json:"projectId"`
	Title       string     `json:"title"`
	Code        string     `json:"code,omitempty"`
	DueAt       *time.Time `json:"dueAt,omitempty"`
	Overdue     bool       `json:"overdue,omitempty"`
	Items       int        `json:"items,omitempty"`
	Declaration string     `json:"declaration,omitempty"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
	// Blocking é quantas pendências impedem o envio do rascunho. Só vem para os rascunhos mais recentes (o cálculo é caro).
	Blocking  *int   `json:"blocking,omitempty"`
	Level     string `json:"level,omitempty"`
	InvitedBy string `json:"invitedBy,omitempty"`
}

// Overview é a resposta de GET /admin/projects/overview.
type Overview struct {
	Counts      OverviewCounts   `json:"counts"`
	ByAccess    OverviewByAccess `json:"byAccess"`
	ActiveCents OverviewCents    `json:"activeCents"`
	Attention   []AttentionItem  `json:"attention"`
}

// Overview resume os projetos da pessoa: contagens, valor em tramitação e o que pede uma ação dela. Só entra o que é dela (titular ou
// membro ativo), mesmo para quem tem leitura ampla, somado ao filtro da permissão. access vazio equivale a AccessFilterAny.
//
// Itens de ação só aparecem para quem pode agir (titular ou membro com edição), exceto o convite novo, que é um aviso e vale para
// qualquer nível. O resumo não arquiva projeto de prazo vencido (isso é da rotina periódica e do settle): marca Overdue.
func (s *Service) Overview(ctx context.Context, actor Actor, perm authz.Filter, access string) (*Overview, error) {
	if access == "" {
		access = AccessFilterAny
	}
	out := &Overview{Attention: []AttentionItem{}}

	// Os totais por tipo de acesso ignoram o filtro pedido: o seletor mostra todos os números de uma vez.
	conds, args, _, err := s.scopeConds(ctx, perm, actor, AccessFilterAny)
	if err != nil {
		return nil, err
	}
	args = append(args, actor.ID)
	me := "$" + strconv.Itoa(len(args))
	err = s.pool.QueryRow(ctx, "SELECT count(*) FILTER (WHERE created_by_id = "+me+"), count(*) FILTER (WHERE created_by_id <> "+me+") FROM projects"+whereOf(conds), args...).
		Scan(&out.ByAccess.Owner, &out.ByAccess.Member)
	if err != nil {
		return nil, err
	}

	base, baseArgs, member, err := s.scopeConds(ctx, perm, actor, access)
	if err != nil {
		return nil, err
	}
	// scoped monta WHERE e argumentos a partir do escopo; act limita a quem pode agir (titular ou membro com edição).
	scoped := func(act bool, extra func(arg func(any) string) []string) (string, []any) {
		cs := append([]string(nil), base...)
		as := append([]any(nil), baseArgs...)
		arg := func(v any) string { as = append(as, v); return "$" + strconv.Itoa(len(as)) }
		if act {
			edit := []string{}
			for id, level := range member {
				if level == LevelEdit {
					edit = append(edit, id.String())
				}
			}
			cs = append(cs, "(created_by_id = "+arg(actor.ID)+" OR id = ANY("+arg(edit)+"::uuid[]))")
		}
		if extra != nil {
			cs = append(cs, extra(arg)...)
		}
		return whereOf(cs), as
	}

	where, as := scoped(false, nil)
	rows, err := s.pool.Query(ctx, `SELECT status, COALESCE(stage, ''), COALESCE(instrument, ''), count(*), COALESCE(sum(total_amount_cents), 0)::bigint
		FROM projects`+where+` GROUP BY 1, 2, 3`, as...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status, stage, instrument string
		var n int
		var cents int64
		if err := rows.Scan(&status, &stage, &instrument, &n, &cents); err != nil {
			rows.Close()
			return nil, err
		}
		out.Counts.add(status, stage, n)
		if status == StatusSubmitted {
			out.ActiveCents.Total += cents
			switch instrument {
			case "convenio":
				out.ActiveCents.Convenio += cents
			case "fomento":
				out.ActiveCents.Fomento += cents
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, load := range []func(context.Context, *Overview, func(bool, func(func(any) string) []string) (string, []any)) error{
		s.attentionComplements, s.attentionSignatures, s.attentionDrafts(actor), s.attentionInvites(actor),
	} {
		if err := load(ctx, out, scoped); err != nil {
			return nil, err
		}
	}
	if len(out.Attention) > maxAttention {
		out.Attention = out.Attention[:maxAttention]
	}
	return out, nil
}

func (c *OverviewCounts) add(status, stage string, n int) {
	switch {
	case status == StatusDraft:
		c.Draft += n
	case status == StatusArchived:
		c.Archived += n
	case stage == StageAwaitingChecklist:
		c.AwaitingChecklist += n
	case stage == StageInAnalysis:
		c.InAnalysis += n
	case stage == StageAwaitingProponent:
		c.AwaitingProponent += n
	case stage == StageAwaitingOpinion:
		c.AwaitingOpinion += n
	}
}

type scopeFn = func(bool, func(func(any) string) []string) (string, []any)

// attentionComplements: projetos devolvidos, com o pedido de correção aberto. O mais próximo do prazo primeiro.
func (s *Service) attentionComplements(ctx context.Context, out *Overview, scoped scopeFn) error {
	where, args := scoped(true, func(func(any) string) []string {
		return []string{"status = 'submitted'", "stage = 'awaiting_proponent'"}
	})
	rows, err := s.pool.Query(ctx, `SELECT id, title, COALESCE(code, ''),
		(SELECT c.due_at FROM project_complement_requests c WHERE c.project_id = projects.id AND c.status = 'open'),
		(SELECT count(*) FROM project_complement_items i JOIN project_complement_requests c ON c.id = i.request_id
			WHERE c.project_id = projects.id AND c.status = 'open')
		FROM projects`+where+` ORDER BY 4 NULLS LAST, id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	now := s.now()
	for rows.Next() {
		it := AttentionItem{Kind: AttentionComplementOpen}
		if err := rows.Scan(&it.ProjectID, &it.Title, &it.Code, &it.DueAt, &it.Items); err != nil {
			return err
		}
		if it.DueAt == nil {
			continue
		}
		it.Overdue = it.DueAt.Before(now)
		out.Attention = append(out.Attention, it)
	}
	return rows.Err()
}

// attentionSignatures: projetos com uma declaração emitida e ainda sem assinatura no MT-ID.
func (s *Service) attentionSignatures(ctx context.Context, out *Overview, scoped scopeFn) error {
	where, args := scoped(true, func(func(any) string) []string {
		return []string{"status <> 'archived'",
			"EXISTS (SELECT 1 FROM project_declarations d WHERE d.project_id = projects.id AND d.status = 'awaiting_signature')"}
	})
	rows, err := s.pool.Query(ctx, `SELECT id, title, COALESCE(code, ''),
		COALESCE((SELECT r.name FROM project_declarations d JOIN document_requirements r ON r.code = d.requirement_code
			WHERE d.project_id = projects.id AND d.status = 'awaiting_signature' ORDER BY d.created_at LIMIT 1), '')
		FROM projects`+where+` ORDER BY updated_at DESC, id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		it := AttentionItem{Kind: AttentionSignaturePend}
		if err := rows.Scan(&it.ProjectID, &it.Title, &it.Code, &it.Declaration); err != nil {
			return err
		}
		out.Attention = append(out.Attention, it)
	}
	return rows.Err()
}

// attentionDrafts: rascunhos em que a pessoa pode trabalhar, dos alterados há menos tempo para os mais antigos. As pendências que
// impedem o envio só são contadas nos mais recentes: o cálculo roda a validação inteira do projeto.
func (s *Service) attentionDrafts(actor Actor) func(context.Context, *Overview, scopeFn) error {
	return func(ctx context.Context, out *Overview, scoped scopeFn) error {
		where, args := scoped(true, func(func(any) string) []string { return []string{"status = 'draft'"} })
		rows, err := s.pool.Query(ctx, `SELECT id, title, updated_at FROM projects`+where+` ORDER BY updated_at DESC, id LIMIT `+strconv.Itoa(maxAttention), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		var items []AttentionItem
		for rows.Next() {
			it := AttentionItem{Kind: AttentionDraft}
			var at time.Time
			if err := rows.Scan(&it.ProjectID, &it.Title, &at); err != nil {
				return err
			}
			it.UpdatedAt = &at
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range items[:min(len(items), maxDraftsWithIssueCount)] {
			row, err := s.q.GetProject(ctx, items[i].ProjectID)
			if err != nil {
				continue
			}
			v, err := s.view(ctx, s.q, row, actor)
			if err != nil {
				if s.log != nil {
					s.log.ErrorContext(ctx, "contando as pendências do rascunho na visão geral", "project", items[i].ProjectID, "error", err)
				}
				continue
			}
			n := 0
			for _, is := range v.Issues {
				if is.Severity == "block" {
					n++
				}
			}
			items[i].Blocking = &n
		}
		out.Attention = append(out.Attention, items...)
		return nil
	}
}

// attentionInvites: projetos compartilhados com a pessoa que ela ainda não abriu. É aviso e vale para qualquer nível de acesso.
func (s *Service) attentionInvites(actor Actor) func(context.Context, *Overview, scopeFn) error {
	return func(ctx context.Context, out *Overview, scoped scopeFn) error {
		rows, err := s.pool.Query(ctx, `SELECT m.project_id, m.level, m.created_at, COALESCE(btrim(inv.first_name || ' ' || inv.last_name), '')
			FROM project_members m JOIN admins a ON a.cpf = m.cpf LEFT JOIN admins inv ON inv.id = m.invited_by_id
			WHERE a.id = $1 AND m.status = 'active' AND m.accepted_at IS NULL ORDER BY m.created_at DESC LIMIT `+strconv.Itoa(maxPendingInvitesInQuery), actor.ID)
		if err != nil {
			return err
		}
		type invite struct {
			level, by string
			at        time.Time
		}
		pending := map[uuid.UUID]invite{}
		ids := []string{}
		for rows.Next() {
			var id uuid.UUID
			var in invite
			if err := rows.Scan(&id, &in.level, &in.at, &in.by); err != nil {
				rows.Close()
				return err
			}
			pending[id] = in
			ids = append(ids, id.String())
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(ids) == 0 {
			return err
		}
		where, args := scoped(false, func(arg func(any) string) []string { return []string{"id = ANY(" + arg(ids) + "::uuid[])"} })
		prows, err := s.pool.Query(ctx, `SELECT id, title, COALESCE(code, '') FROM projects`+where, args...)
		if err != nil {
			return err
		}
		defer prows.Close()
		var items []AttentionItem
		for prows.Next() {
			it := AttentionItem{Kind: AttentionSharedNew}
			if err := prows.Scan(&it.ProjectID, &it.Title, &it.Code); err != nil {
				return err
			}
			it.Level, it.InvitedBy = pending[it.ProjectID].level, pending[it.ProjectID].by
			items = append(items, it)
		}
		if err := prows.Err(); err != nil {
			return err
		}
		sort.Slice(items, func(i, j int) bool { return pending[items[i].ProjectID].at.After(pending[items[j].ProjectID].at) })
		out.Attention = append(out.Attention, items...)
		return nil
	}
}

func whereOf(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}
