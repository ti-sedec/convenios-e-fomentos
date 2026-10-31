package project

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// Seções que a equipe pode devolver (o mesmo nome do caminho da API).
var returnableSections = map[string]string{
	"proponent": "Proponente", "institution": "Instituição", "proposal": "Proposta", "funding": "Recursos", "document-options": "Situações dos documentos",
}

// sectionRight converte o rótulo de auditoria da seção no nome que a equipe usa para devolvê-la.
func sectionRight(section string) string {
	if section == "documents" {
		return "document-options"
	}
	return section
}

type openComplement struct {
	id       uuid.UUID
	sections map[string]bool
	docs     map[string]bool
	extras   map[string]bool // id do documento adicional devolvido
}

// rights diz o que o proponente pode alterar agora: no rascunho, tudo; depois do envio, só o que a equipe devolveu, enquanto o
// prazo de complementação não vence (e documentos que ficaram pendentes por causa das correções); no mais, nada.
type rights struct {
	draft bool
	open  *openComplement
}

func (r rights) editable() bool { return r.draft || r.open != nil }

func (r rights) canSection(name string) bool {
	return r.draft || (r.open != nil && r.open.sections[name])
}

// canDocument: missing indica que o documento está pendente no checklist (precisa de resposta mesmo sem ter sido devolvido).
func (r rights) canDocument(key string, missing bool) bool {
	return r.draft || (r.open != nil && (r.open.docs[key] || missing))
}

func (s *Service) rightsOf(ctx context.Context, q *db.Queries, cur db.Project) (rights, error) {
	if cur.Status == StatusDraft {
		return rights{draft: true}, nil
	}
	if cur.Status != StatusSubmitted || str(cur.Stage) != StageAwaitingProponent {
		return rights{}, nil
	}
	req, err := q.GetOpenComplementRequest(ctx, cur.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && req.DueAt.Before(s.now())) {
		return rights{}, nil
	}
	if err != nil {
		return rights{}, err
	}
	items, err := q.ListComplementItems(ctx, req.ID)
	if err != nil {
		return rights{}, err
	}
	o := &openComplement{id: req.ID, sections: map[string]bool{}, docs: map[string]bool{}, extras: map[string]bool{}}
	for _, it := range items {
		switch it.Kind {
		case "section":
			o.sections[it.Target] = true
		case "extra":
			o.extras[it.Target] = true
		default:
			o.docs[it.Target] = true
		}
	}
	return rights{open: o}, nil
}

func errNotReturned() error {
	return httpapi.ErrConflict("not_returned", "Este item não foi devolvido para correção e não pode ser alterado.")
}

// markAnswered registra que o proponente mexeu num item devolvido (só tem efeito durante uma complementação).
func (s *Service) markAnswered(ctx context.Context, q *db.Queries, projectID uuid.UUID, cur db.Project, kind, target string) error {
	if cur.Status == StatusDraft {
		return nil
	}
	return q.MarkComplementItemAnswered(ctx, db.MarkComplementItemAnsweredParams{ProjectID: projectID, Kind: kind, Target: target})
}

// docKey identifica um documento do checklist: o código, mais "@" e a emenda nas exigências por emenda.
func docKey(code string, amendment *uuid.UUID) string {
	if amendment == nil {
		return code
	}
	return code + "@" + amendment.String()
}

// settle arquiva o projeto cujo prazo de complementação venceu, antes de qualquer leitura ou gravação (a rotina periódica
// faria o mesmo, mas o proponente não deve poder alterar nada depois do prazo só porque ela ainda não rodou).
func (s *Service) settle(ctx context.Context, id uuid.UUID) {
	cur, err := s.q.GetProject(ctx, id)
	if err != nil || cur.Status != StatusSubmitted || str(cur.Stage) != StageAwaitingProponent {
		return
	}
	req, err := s.q.GetOpenComplementRequest(ctx, id)
	if err != nil || !req.DueAt.Before(s.now()) {
		return
	}
	if _, err := s.ExpireComplements(ctx); err != nil && s.log != nil {
		s.log.ErrorContext(ctx, "arquivando projetos com prazo de complementação vencido", "error", err)
	}
}
