package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
)

// Motivos de versão e de pedido de complementação.
const (
	ReasonComplement = "complement"

	ComplementOpen      = "open"
	ComplementAnswered  = "answered"
	ComplementExpired   = "expired"
	ComplementCancelled = "cancelled"

	defaultComplementDays = 3
	maxComplementItems    = 40
	maxNote               = 500
	maxMessage            = 2000
	maxArchiveNote        = 1000
	minArchiveNote        = 3
)

// ---- Leitura: pedido de complementação, arquivamento e origem ----

func (s *Service) complementView(ctx context.Context, q *db.Queries, r db.Project, cl *Checklist) (*Complement, error) {
	req, err := q.GetLatestComplementRequest(ctx, r.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items, err := q.ListComplementItems(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	for _, it := range cl.Items {
		l := it.Name
		if it.AmendmentLabel != "" {
			l += " (" + it.AmendmentLabel + ")"
		}
		labels[it.key()] = l
	}
	extras := map[string]string{}
	for _, x := range cl.ExtraDocuments {
		extras[x.ID.String()] = x.Title
	}
	c := &Complement{Number: int(req.Number), Status: req.Status, RequestedAt: req.RequestedAt, DueAt: req.DueAt, Message: req.Message,
		Overdue: req.Status == ComplementOpen && req.DueAt.Before(s.now()), Items: make([]ComplementItem, len(items))}
	for i, it := range items {
		label := labels[it.Target]
		switch it.Kind {
		case "section":
			label = returnableSections[it.Target]
		case "extra":
			label = extras[it.Target]
		}
		if label == "" {
			label = it.Target
		}
		c.Items[i] = ComplementItem{Kind: it.Kind, Target: it.Target, Label: label, Note: it.Note, Answered: it.AnsweredAt != nil}
	}
	return c, nil
}

func (s *Service) originView(ctx context.Context, q *db.Queries, r db.Project) string {
	if r.OriginProjectID == nil {
		return ""
	}
	o, err := q.GetProject(ctx, *r.OriginProjectID)
	if err != nil {
		return ""
	}
	return str(o.Code)
}

// ---- Pedido de complementação (equipe) ----

// ComplementItemInput é um item devolvido ao proponente.
type ComplementItemInput struct {
	Kind   string `json:"kind"`   // section | document | extra
	Target string `json:"target"` // nome da seção, "codigo" / "codigo@id-da-emenda", ou o id do documento adicional
	Note   string `json:"note"`
}

// ComplementInput é o corpo do pedido de complementação.
type ComplementInput struct {
	Message string                `json:"message"`
	Items   []ComplementItemInput `json:"items"`
}

// dueAt é o fim do prazo: 23:59:59 (Cuiabá) do último dos N dias corridos contados do dia do pedido.
func (s *Service) dueAt(days int) time.Time {
	t := s.today()
	return time.Date(t.Year(), t.Month(), t.Day()+days, 23, 59, 59, 0, cuiaba())
}

// RequestComplement devolve itens ao proponente: o projeto vai para "aguardando proponente" e o prazo começa a correr.
func (s *Service) RequestComplement(ctx context.Context, actor Actor, id uuid.UUID, in ComplementInput, dec authz.Decision) (*Project, error) {
	in.Message = multiline(in.Message)
	var p problems
	if tooLong(in.Message, maxMessage) {
		p.add("message", "Use até 2.000 caracteres.")
	}
	if len(in.Items) == 0 || len(in.Items) > maxComplementItems {
		p.add("items", "Devolva de 1 a 40 itens.")
	}
	if err := p.err(); err != nil {
		return nil, err
	}
	var out *Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if !dec.AllowsRecord(refOf(cur).Getter()) {
			return httpapi.ErrForbidden()
		}
		if cur.Status != StatusSubmitted {
			return httpapi.ErrConflict("project_not_submitted", "Só projetos enviados e ativos recebem pedido de complementação.")
		}
		if framingPending(cur) {
			return errFramingPending()
		}
		if st := str(cur.Stage); st != StageAwaitingChecklist && st != StageInAnalysis {
			return errStage(st)
		}
		if err := s.ensureResponsible(ctx, q, cur, actor); err != nil {
			return err
		}
		_, cl, err := s.viewWithChecklist(ctx, q, cur, actor)
		if err != nil {
			return err
		}
		valid := map[string]bool{}
		for _, it := range cl.Items {
			valid["document:"+it.key()] = true
		}
		for _, x := range cl.ExtraDocuments {
			valid["extra:"+x.ID.String()] = true
		}
		for sec := range returnableSections {
			valid["section:"+sec] = true
		}
		seen := map[string]bool{}
		for i, it := range in.Items {
			f := fmt.Sprintf("items[%d]", i)
			k := it.Kind + ":" + it.Target
			note := multiline(it.Note)
			switch {
			case it.Kind != "section" && it.Kind != "document" && it.Kind != "extra":
				p.add(f+".kind", "Use section, document ou extra.")
			case !valid[k]:
				p.add(f+".target", "Item inexistente neste projeto.")
			case seen[k]:
				p.add(f+".target", "Item repetido.")
			case tooLong(note, maxNote):
				p.add(f+".note", "Use até 500 caracteres.")
			}
			seen[k] = true
		}
		if err := p.err(); err != nil {
			return err
		}
		snap, err := s.rules.SnapshotAt(ctx, q, s.today())
		if err != nil {
			return err
		}
		days := snap.ComplementDays
		if days <= 0 {
			days = defaultComplementDays
		}
		n, err := q.NextComplementNumber(ctx, id)
		if err != nil {
			return err
		}
		req, err := q.CreateComplementRequest(ctx, db.CreateComplementRequestParams{ProjectID: id, Number: n, RequestedByID: &actor.ID, DueAt: s.dueAt(days), Message: in.Message})
		if err != nil {
			return err
		}
		for _, it := range in.Items {
			if err := q.AddComplementItem(ctx, db.AddComplementItemParams{RequestID: req.ID, Kind: it.Kind, Target: it.Target, Note: multiline(it.Note)}); err != nil {
				return err
			}
			// O que volta ao proponente deixa de estar aprovado; o resto da conferência fica como está.
			if err := q.ResetItemReview(ctx, db.ResetItemReviewParams{ProjectID: id, Kind: it.Kind, Target: it.Target}); err != nil {
				return err
			}
		}
		// Sem responsável, quem devolve o projeto passa a ser o analista dele: o reenvio volta para essa pessoa.
		if cur.AnalystID == nil {
			if _, err := q.SetProjectAnalyst(ctx, db.SetProjectAnalystParams{ID: id, AnalystID: &actor.ID}); err != nil {
				return notFound(err)
			}
		}
		row, err := q.SetProjectStage(ctx, db.SetProjectStageParams{ID: id, Stage: ptr(StageAwaitingProponent), UpdatedByID: &actor.ID})
		if err != nil {
			return notFound(err)
		}
		if out, err = s.view(ctx, q, row, actor); err != nil {
			return err
		}
		if err := s.transition(ctx, q, row, actor.entry("project.complement-request", id, map[string]any{"number": n, "items": len(in.Items), "dueAt": req.DueAt})); err != nil {
			return err
		}
		return s.notifyComplementRequested(ctx, q, out)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// ---- Reenvio com as correções (proponente) ----

// Resubmit reenvia o projeto depois de uma complementação: confere as pendências, a versão lida e a declaração, guarda uma nova
// versão imutável e devolve o projeto para a análise. Os itens devolvidos são respondidos pelas próprias gravações de cada
// seção e documento; o reenvio não exige que todos tenham mudado (a equipe confere).
func (s *Service) Resubmit(ctx context.Context, actor Actor, id uuid.UUID, in SubmitInput, sub authz.Decision) (*Project, error) {
	if !in.Declaration {
		return nil, fieldIssue("declaration", "É preciso aceitar a declaração de veracidade para reenviar o projeto.")
	}
	s.settle(ctx, id)
	var (
		out     *Project
		pending []string
		number  int32
	)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if !sub.AllowsRecord(refOf(cur).Getter()) {
			return httpapi.ErrForbidden()
		}
		rt, err := s.rightsOf(ctx, q, cur)
		if err != nil {
			return err
		}
		if rt.open == nil {
			return httpapi.ErrConflict("project_not_awaiting", "Este projeto não está aguardando correções do proponente.")
		}
		if cur.Version != in.Version {
			return errVersionConflict(cur.Version)
		}
		view, _, err := s.viewWithChecklist(ctx, q, cur, actor)
		if err != nil {
			return err
		}
		var blocks []Issue
		for _, i := range view.Issues {
			if i.Severity == SeverityBlock {
				blocks = append(blocks, i)
			}
		}
		if len(blocks) > 0 {
			return errHasIssues(blocks)
		}
		if cur.SigconNumber != nil {
			n, err := q.CountOtherSubmittedProjectsWithSigconNumber(ctx, db.CountOtherSubmittedProjectsWithSigconNumberParams{SigconNumber: cur.SigconNumber, ID: id})
			if err != nil {
				return err
			}
			if n > 0 {
				return fieldIssue("sigconNumber", "Já existe um projeto enviado com este número de proposta do SIGCon.")
			}
		}
		snap, err := s.rules.SnapshotAt(ctx, q, s.today())
		if err != nil {
			return err
		}
		if pending, err = q.DeleteProjectPendingFiles(ctx, id); err != nil {
			return err
		}
		if err := q.LockProjectFiles(ctx, id); err != nil {
			return err
		}
		if number, err = q.NextProjectVersionNumber(ctx, id); err != nil {
			return err
		}
		row, err := q.SetProjectStage(ctx, db.SetProjectStageParams{ID: id, Stage: ptr(StageInAnalysis), UpdatedByID: &actor.ID})
		if err != nil {
			return notFound(err)
		}
		if err := q.AnswerComplementRequest(ctx, db.AnswerComplementRequestParams{ID: rt.open.id, AnsweredVersion: &number}); err != nil {
			return err
		}
		final, cl, err := s.viewWithChecklist(ctx, q, row, actor)
		if err != nil {
			return err
		}
		now := s.now()
		decl := Declaration{Text: DeclarationText, AcceptedAt: now, ByName: actor.Name, ByCPF: cpf.Mask(actor.CPF), IP: actor.IP}
		raw, err := json.Marshal(VersionSnapshot{Project: *final, Documents: *cl, Rules: *snap, Declaration: decl})
		if err != nil {
			return err
		}
		if _, err := q.InsertProjectVersion(ctx, db.InsertProjectVersionParams{ProjectID: id, Number: number, Reason: ReasonComplement, SubmittedAt: now,
			SubmittedByID: &actor.ID, SubmissionIp: actor.IP, Snapshot: raw}); err != nil {
			return err
		}
		if err := s.transition(ctx, q, row, actor.entry("project.resubmit", id, map[string]any{"version": number})); err != nil {
			return err
		}
		if err := s.notifyResubmitted(ctx, q, final, int(number)); err != nil {
			return err
		}
		out = final
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.deleteObjects(ctx, pending)
	return out, nil
}

// ---- Arquivamento ----

// ArchiveInput é o corpo do arquivamento. Reason só vale na rota da equipe (missing_documents ou other).
type ArchiveInput struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

func cleanNote(note string) (string, error) {
	note = multiline(note)
	if n := len([]rune(note)); n < minArchiveNote || n > maxArchiveNote {
		return "", fieldIssue("note", "Informe o motivo, de 3 a 1.000 caracteres.")
	}
	return note, nil
}

// archive é o arquivamento feito por uma pessoa: confere o alcance da permissão e a situação do projeto.
// byAnalyst marca o arquivamento feito como Analista: ele é bloqueado com enquadramento pendente e só o analista responsável o faz.
// A desistência e a negação não passam por essas regras.
func (s *Service) archive(ctx context.Context, actor Actor, id uuid.UUID, reason, note string, dec authz.Decision, byAnalyst bool) (*Project, error) {
	note, err := cleanNote(note)
	if err != nil {
		return nil, err
	}
	s.settle(ctx, id)
	var out *Project
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if !dec.AllowsRecord(refOf(cur).Getter()) {
			return httpapi.ErrForbidden()
		}
		switch cur.Status {
		case StatusDraft:
			return httpapi.ErrConflict("project_not_submitted", "Rascunho não se arquiva: exclua-o.")
		case StatusArchived:
			return httpapi.ErrConflict("project_archived", "Este projeto já está arquivado.")
		}
		if byAnalyst {
			if framingPending(cur) {
				return errFramingPending()
			}
			if err := s.ensureResponsible(ctx, q, cur, actor); err != nil {
				return err
			}
		}
		row, err := s.archiveRow(ctx, q, cur, &actor.ID, reason, note)
		if err != nil {
			return err
		}
		if out, err = s.view(ctx, q, row, actor); err != nil {
			return err
		}
		if err := s.transition(ctx, q, row, actor.entry("project.archive", id, map[string]any{"reason": reason})); err != nil {
			return err
		}
		return s.notifyArchived(ctx, q, out, row.DepartmentID, row.AnalystID)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// archiveRow arquiva o projeto e encerra o pedido de complementação aberto. Quem chama já travou a linha.
func (s *Service) archiveRow(ctx context.Context, q *db.Queries, cur db.Project, by *uuid.UUID, reason, note string) (db.Project, error) {
	closed := ComplementCancelled
	if reason == ArchiveComplementDeadline {
		closed = ComplementExpired
	}
	if err := q.CloseOpenComplementRequests(ctx, db.CloseOpenComplementRequestsParams{ProjectID: cur.ID, Status: closed}); err != nil {
		return db.Project{}, err
	}
	row, err := q.ArchiveProject(ctx, db.ArchiveProjectParams{ID: cur.ID, ArchivedByID: by, ArchiveReason: &reason, ArchiveNote: note})
	return row, notFound(err)
}

// Withdraw é a desistência do proponente: o projeto é arquivado, com o motivo informado.
func (s *Service) Withdraw(ctx context.Context, actor Actor, id uuid.UUID, note string, dec authz.Decision) (*Project, error) {
	return s.archive(ctx, actor, id, ArchiveWithdrawn, note, dec, false)
}

// Deny é a negativa do Secretário: o projeto é arquivado sem passar por parecer.
func (s *Service) Deny(ctx context.Context, actor Actor, id uuid.UUID, note string, dec authz.Decision) (*Project, error) {
	return s.archive(ctx, actor, id, ArchiveDenied, note, dec, false)
}

// Archive é o arquivamento pela equipe por falta de documentos obrigatórios ou outro motivo.
func (s *Service) Archive(ctx context.Context, actor Actor, id uuid.UUID, in ArchiveInput, dec authz.Decision) (*Project, error) {
	if in.Reason != ArchiveMissingDocuments && in.Reason != ArchiveOther {
		return nil, fieldIssue("reason", "Use missing_documents ou other.")
	}
	return s.archive(ctx, actor, id, in.Reason, in.Note, dec, true)
}

// ExpireComplements arquiva os projetos cujo prazo de complementação venceu. Pode rodar em várias instâncias ao mesmo tempo
// (cada pedido é travado por quem o processa). Devolve quantos projetos arquivou.
func (s *Service) ExpireComplements(ctx context.Context) (int, error) {
	archived := 0
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		due, err := q.LockDueComplementRequests(ctx)
		if err != nil {
			return err
		}
		for _, d := range due {
			cur, err := q.GetProjectForUpdate(ctx, d.ProjectID)
			if err != nil {
				return err
			}
			if cur.Status != StatusSubmitted || str(cur.Stage) != StageAwaitingProponent {
				// Estado inconsistente: só encerra o pedido.
				if err := q.CloseOpenComplementRequests(ctx, db.CloseOpenComplementRequestsParams{ProjectID: cur.ID, Status: ComplementCancelled}); err != nil {
					return err
				}
				continue
			}
			note := "O prazo para o proponente enviar as correções venceu em " + s.complementDueText(ctx, q, cur.ID) + "."
			row, err := s.archiveRow(ctx, q, cur, nil, ArchiveComplementDeadline, note)
			if err != nil {
				return err
			}
			if err := s.transition(ctx, q, row, auditSystem("project.archive", row.ID, map[string]any{"reason": ArchiveComplementDeadline})); err != nil {
				return err
			}
			p, err := s.view(ctx, q, row, Actor{})
			if err != nil {
				return err
			}
			if err := s.notifyArchived(ctx, q, p, row.DepartmentID, row.AnalystID); err != nil {
				return err
			}
			archived++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return archived, nil
}

func (s *Service) complementDueText(ctx context.Context, q *db.Queries, id uuid.UUID) string {
	req, err := q.GetLatestComplementRequest(ctx, id)
	if err != nil {
		return "data anterior"
	}
	return req.DueAt.In(cuiaba()).Format("02/01/2006 15:04")
}

// ---- Duplicar ----

// DuplicateInput é o corpo da duplicação.
type DuplicateInput struct {
	// CopyDocuments copia também os arquivos e as justificativas. Por padrão não: certidões e comprovantes vencem.
	CopyDocuments bool `json:"copyDocuments"`
}

// Duplicate cria um rascunho novo com os dados de um projeto do próprio proponente (em qualquer situação, inclusive arquivado).
// O projeto de origem não muda. Os documentos só são copiados a pedido.
func (s *Service) Duplicate(ctx context.Context, actor Actor, id uuid.UUID, in DuplicateInput) (*Project, error) {
	if in.CopyDocuments {
		if err := s.requireStore(); err != nil {
			return nil, err
		}
	}
	var (
		out    *Project
		copied []string // objetos já copiados: apagados se a transação falhar
	)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		src, err := q.GetProject(ctx, id)
		if err != nil || src.CreatedByID != actor.ID {
			return httpapi.ErrNotFound()
		}
		row, err := q.DuplicateProject(ctx, db.DuplicateProjectParams{ID: id, CreatedByID: actor.ID, ProponentName: actor.Name, ProponentCpf: actor.CPF})
		if err != nil {
			return err
		}
		if err := q.CopyProjectExecutionPeriods(ctx, db.CopyProjectExecutionPeriodsParams{SourceProjectID: id, NewProjectID: row.ID}); err != nil {
			return err
		}
		// Emendas: ids novos; o mapa leva os documentos por emenda para a emenda correspondente.
		amendMap := map[uuid.UUID]uuid.UUID{}
		ams, err := q.ListProjectAmendments(ctx, id)
		if err != nil {
			return err
		}
		for i, a := range ams {
			nid := uuid.New()
			amendMap[a.ID] = nid
			if err := q.CopyProjectAmendment(ctx, db.CopyProjectAmendmentParams{ID: nid, ProjectID: row.ID, Position: int32(i), ParliamentarianID: a.ParliamentarianID, //nolint:gosec // até 20
				ParliamentarianFreeName: a.ParliamentarianFreeName, AmendmentNumber: a.AmendmentNumber, Year: a.Year, AmountCents: a.AmountCents}); err != nil {
				return err
			}
		}
		if in.CopyDocuments {
			if copied, err = s.copyDocuments(ctx, q, actor, src.ID, row.ID, amendMap); err != nil {
				return err
			}
		}
		if out, err = s.view(ctx, q, row, actor); err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("project.duplicate", row.ID, map[string]any{"origin": id, "copyDocuments": in.CopyDocuments}))
	})
	if err != nil {
		s.deleteObjects(ctx, copied)
		return nil, err
	}
	return out, nil
}

// copyDocuments copia as justificativas e os arquivos (objetos e registros) do projeto de origem para o novo.
func (s *Service) copyDocuments(ctx context.Context, q *db.Queries, actor Actor, from, to uuid.UUID, amendMap map[uuid.UUID]uuid.UUID) ([]string, error) {
	var copied []string
	items, err := q.ListProjectDocumentItems(ctx, from)
	if err != nil {
		return copied, err
	}
	files, err := q.ListProjectFiles(ctx, from)
	if err != nil {
		return copied, err
	}
	newItem := map[uuid.UUID]uuid.UUID{}
	for _, it := range items {
		var amendment *uuid.UUID
		if it.AmendmentID != nil {
			m, ok := amendMap[*it.AmendmentID]
			if !ok {
				continue
			}
			amendment = &m
		}
		created, err := q.CreateProjectDocumentItemCopy(ctx, db.CreateProjectDocumentItemCopyParams{ProjectID: to, RequirementCode: it.RequirementCode, AmendmentID: amendment, Justification: it.Justification, Title: it.Title, Description: it.Description})
		if err != nil {
			return copied, err
		}
		newItem[it.ID] = created.ID
	}
	for _, f := range files {
		item, ok := newItem[f.ItemID]
		if !ok || f.Status != FileConfirmed {
			continue
		}
		id := uuid.New()
		key := fmt.Sprintf("projects/%s/%s", to, id)
		if err := s.store.Copy(ctx, f.ObjectKey, key); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return copied, httpapi.ErrConflict("file_missing", fmt.Sprintf("O arquivo %q não está mais no armazenamento e não pôde ser copiado.", f.Filename))
			}
			return copied, fmt.Errorf("copiando %q: %w", f.Filename, err)
		}
		copied = append(copied, key)
		if _, err := q.InsertProjectFile(ctx, db.InsertProjectFileParams{ID: id, ItemID: item, ObjectKey: key, Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.SizeBytes, CreatedByID: &actor.ID}); err != nil {
			return copied, err
		}
		if _, err := q.ConfirmProjectFile(ctx, db.ConfirmProjectFileParams{ID: id, SizeBytes: f.SizeBytes, Sha256: f.Sha256}); err != nil {
			return copied, err
		}
	}
	return copied, nil
}

// CleanupAbandonedDrafts apaga os rascunhos sem nenhuma alteração há mais de days dias, com os arquivos e todo o resto (retenção
// de dados pessoais). Projeto enviado ou arquivado nunca é apagado por tempo. days <= 0 não faz nada. Devolve quantos apagou.
func (s *Service) CleanupAbandonedDrafts(ctx context.Context, days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	ids, err := s.q.ListAbandonedDrafts(ctx, int32(days)) //nolint:gosec // dias, valor pequeno
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		var keys []string
		err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			cur, err := q.GetProjectForUpdate(ctx, id)
			if err != nil {
				return err
			}
			// Conferido de novo com a linha travada: o proponente pode ter voltado ao rascunho nesse meio-tempo.
			if cur.Status != StatusDraft || cur.UpdatedAt.After(s.now().AddDate(0, 0, -days)) {
				return nil
			}
			if keys, err = q.ListProjectFileKeys(ctx, id); err != nil {
				return err
			}
			if _, err := q.DeleteProject(ctx, id); err != nil {
				return err
			}
			n++
			return s.audit.Record(ctx, q, auditSystem("project.draft-expired", id, map[string]any{"days": days}))
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return n, err
		}
		s.deleteObjects(ctx, keys)
	}
	return n, nil
}
