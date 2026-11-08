package project

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// maxUnopenedListed limita quantos arquivos o erro file_not_opened lista (o total vai junto).
const maxUnopenedListed = 20

// AnalystInfo é o analista responsável, na visão da equipe. IsMe diz se é quem consulta (só ele continua a análise).
type AnalystInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	IsMe bool      `json:"isMe"`
}

// ReviewInput é o corpo de PUT /review/items: aprova, reprova (com justificativa) ou desfaz a decisão sobre um item.
type ReviewInput struct {
	Kind   string `json:"kind"`   // section | document | extra
	Target string `json:"target"` // seção, chave do documento ou id do documento adicional
	// Decision é approved, rejected ou pending (desfaz). Vazio vale o campo antigo Approved.
	Decision string `json:"decision"`
	// Note é a justificativa da reprovação: obrigatória com rejected, ignorada nos demais.
	Note string `json:"note"`
	// Approved é o formato anterior: true aprova, false desfaz. Prefira Decision.
	Approved bool `json:"approved"`
}

// decision resolve a decisão pedida, aceitando o formato anterior.
func (in ReviewInput) decision() string {
	if in.Decision != "" {
		return in.Decision
	}
	if in.Approved {
		return ReviewApproved
	}
	return ReviewPending
}

func errStageForReview(stage string) error {
	if stage == StageAwaitingChecklist {
		return httpapi.ErrConflict("analysis_not_started", "Assuma a análise do projeto antes de conferir os itens.")
	}
	return errStage(stage)
}

// lockedForReview trava o projeto e confere permissão e situação (enviado e ativo).
func (s *Service) lockedForReview(ctx context.Context, q *db.Queries, id uuid.UUID, dec authz.Decision) (db.Project, error) {
	cur, err := q.GetProjectForUpdate(ctx, id)
	if err != nil {
		return db.Project{}, notFound(err)
	}
	if !dec.AllowsRecord(refOf(cur).Getter()) {
		return db.Project{}, httpapi.ErrForbidden()
	}
	if cur.Status != StatusSubmitted {
		return db.Project{}, httpapi.ErrConflict("project_not_submitted", "Só projetos enviados e ativos têm conferência.")
	}
	return cur, nil
}

// ensureResponsible garante que quem age é o analista responsável. Sem responsável a regra não se aplica (quem chama decide se a ação
// o torna responsável). Com outro responsável a ação é recusada: para continuar a análise é preciso assumir o projeto (TakeOverAnalysis).
// Vale para qualquer pessoa que aja como Analista, inclusive o Super Admin.
func (s *Service) ensureResponsible(ctx context.Context, q *db.Queries, cur db.Project, actor Actor) error {
	if cur.AnalystID == nil || *cur.AnalystID == actor.ID {
		return nil
	}
	name := "outro analista"
	if a, err := q.GetAdminByID(ctx, *cur.AnalystID); err == nil {
		name = adminDisplayName(a)
	}
	return httpapi.ErrConflict("not_responsible_analyst", fmt.Sprintf("Este projeto está com %s. Assuma o projeto para continuar a análise.", name))
}

// adminDisplayName é o nome da pessoa ou, sem nome, o e-mail.
func adminDisplayName(a db.Admin) string {
	if name := strings.TrimSpace(a.FirstName + " " + a.LastName); name != "" {
		return name
	}
	return a.Email
}

// StartAnalysis inicia a análise: "Aguardando análise de checklist" passa a "Em análise" e quem iniciou vira o responsável.
// É idempotente para o próprio responsável. Se o projeto já está com outro analista a resposta é 409 not_responsible_analyst: para
// continuar a análise é preciso assumir o projeto (TakeOverAnalysis). Se o projeto está em análise mas sem responsável (voltou de
// uma complementação pedida por quem já saiu, ou de registro antigo), o primeiro a iniciar vira o responsável.
func (s *Service) StartAnalysis(ctx context.Context, actor Actor, id uuid.UUID, dec authz.Decision) (*Project, error) {
	var out *Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := s.lockedForReview(ctx, q, id, dec)
		if err != nil {
			return err
		}
		row, changed := cur, false
		switch str(cur.Stage) {
		case StageAwaitingChecklist:
			if row, err = q.AssumeProjectAnalysis(ctx, db.AssumeProjectAnalysisParams{ID: id, AnalystID: &actor.ID}); err != nil {
				return notFound(err)
			}
			changed = true
		case StageInAnalysis:
			if cur.AnalystID != nil {
				if err := s.ensureResponsible(ctx, q, cur, actor); err != nil {
					return err
				}
				break // já é o responsável: nada muda
			}
			if row, err = q.ClaimProjectAnalyst(ctx, db.ClaimProjectAnalystParams{ID: id, AnalystID: &actor.ID}); err != nil {
				return notFound(err)
			}
			changed = true
		default:
			return errStage(str(cur.Stage))
		}
		if changed {
			if err := s.transition(ctx, q, row, actor.entry("project.start-analysis", id, map[string]any{"stage": StageInAnalysis})); err != nil {
				return err
			}
		}
		out, err = s.view(ctx, q, row, actor)
		return err
	})
	return out, err
}

// TakeOverAnalysis é o ato de assumir ativamente um projeto que está com outro analista: quem chama passa a ser o responsável e o
// anterior perde as ações (e é avisado por e-mail). Vale com o projeto em análise ou aguardando o proponente; não vale antes de
// iniciar a análise (usa-se "iniciar"), com o enquadramento pendente (o projeto está com o Revisor) nem aguardando o parecer.
// Idempotente para quem já é o responsável. A conferência já feita continua como está.
func (s *Service) TakeOverAnalysis(ctx context.Context, actor Actor, id uuid.UUID, dec authz.Decision) (*Project, error) {
	var out *Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := s.lockedForReview(ctx, q, id, dec)
		if err != nil {
			return err
		}
		switch str(cur.Stage) {
		case StageInAnalysis, StageAwaitingProponent:
		case StageAwaitingChecklist:
			return errStageForReview(StageAwaitingChecklist)
		default:
			return errStage(str(cur.Stage))
		}
		if framingPending(cur) {
			return errFramingPending()
		}
		row := cur
		if cur.AnalystID == nil || *cur.AnalystID != actor.ID {
			if row, err = q.SetProjectAnalyst(ctx, db.SetProjectAnalystParams{ID: id, AnalystID: &actor.ID}); err != nil {
				return notFound(err)
			}
			detail := map[string]any{"to": actor.ID}
			if cur.AnalystID != nil {
				detail["from"] = *cur.AnalystID
			}
			if err := s.transition(ctx, q, row, actor.entry(actionTakeOver, id, detail)); err != nil {
				return err
			}
			if cur.AnalystID != nil {
				if out, err = s.view(ctx, q, row, actor); err != nil {
					return err
				}
				return s.notifyTakenOver(ctx, q, out, *cur.AnalystID, actor, row.UpdatedAt)
			}
		}
		out, err = s.view(ctx, q, row, actor)
		return err
	})
	return out, err
}

// ReviewItem aprova ou desfaz a aprovação de um item da conferência. Exige o projeto em análise. Para aprovar um documento com
// arquivo, quem aprova precisa ter aberto cada arquivo confirmado atual do item (ver recordStaffOpening).
func (s *Service) ReviewItem(ctx context.Context, actor Actor, id uuid.UUID, in ReviewInput, dec authz.Decision) (*Project, error) {
	in.Target = strings.TrimSpace(in.Target)
	switch in.Kind {
	case ReviewSection, ReviewDocument, ReviewExtra:
	default:
		return nil, fieldIssue("kind", "Use section, document ou extra.")
	}
	decision := in.decision()
	note := multiline(in.Note)
	switch decision {
	case ReviewApproved, ReviewPending:
	case ReviewRejected:
		// Documento adicional não é devolvido ao proponente (o pedido de complementação não o aceita): só se aprova.
		if in.Kind == ReviewExtra {
			return nil, fieldIssue("kind", "Documento adicional só pode ser aprovado.")
		}
		if note == "" {
			return nil, fieldIssue("note", "Informe a justificativa da reprovação.")
		}
		if utf8.RuneCountInString(note) > maxReviewNote {
			return nil, fieldIssue("note", fmt.Sprintf("A justificativa pode ter até %d caracteres.", maxReviewNote))
		}
	default:
		return nil, fieldIssue("decision", "Use approved, rejected ou pending.")
	}
	var out *Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := s.lockedForReview(ctx, q, id, dec)
		if err != nil {
			return err
		}
		if str(cur.Stage) != StageInAnalysis {
			return errStageForReview(str(cur.Stage))
		}
		if framingPending(cur) {
			return errFramingPending()
		}
		if err := s.ensureResponsible(ctx, q, cur, actor); err != nil {
			return err
		}
		_, cl, err := s.viewWithChecklist(ctx, q, cur, actor)
		if err != nil {
			return err
		}
		t, ok := findTarget(reviewTargets(cl), in.Kind, in.Target)
		if !ok || t.Blocking {
			return &httpapi.APIError{Status: 422, Code: "item_not_reviewable", Message: "Este item não pode ser conferido agora.",
				Details: map[string]any{"fields": []httpapi.FieldError{{Field: "target", Message: "Item inexistente ou sem o que conferir (documento obrigatório sem arquivo nem justificativa)."}}}}
		}
		if decision != ReviewPending {
			// Reprovar exige ter visto cada arquivo do item; aprovar, ter chegado ao fim de cada um: não se julga o que não se viu.
			seen, err := s.openedBy(ctx, q, id, actor.ID)
			if err != nil {
				return err
			}
			if missing := unseen(t, seen, false); len(missing) > 0 {
				return errFileNotOpened(missing, decision)
			}
			if decision == ReviewApproved {
				if missing := unseen(t, seen, true); len(missing) > 0 {
					return errFileNotRead(missing)
				}
			}
		}
		switch decision {
		case ReviewApproved:
			err = q.ApproveItemReview(ctx, db.ApproveItemReviewParams{ProjectID: id, Kind: in.Kind, Target: in.Target, ReviewedByID: &actor.ID})
		case ReviewRejected:
			err = q.RejectItemReview(ctx, db.RejectItemReviewParams{ProjectID: id, Kind: in.Kind, Target: in.Target, Note: &note, ReviewedByID: &actor.ID})
		default:
			err = q.ResetItemReview(ctx, db.ResetItemReviewParams{ProjectID: id, Kind: in.Kind, Target: in.Target})
		}
		if err != nil {
			return err
		}
		// A justificativa fica no item; a auditoria não guarda texto livre (pode trazer dado pessoal), só a decisão.
		detail := map[string]any{"kind": in.Kind, "target": in.Target, "decision": decision, "approved": decision == ReviewApproved}
		if err := s.audit.Record(ctx, q, actor.entry("project.review-item", id, detail)); err != nil {
			return err
		}
		out, err = s.view(ctx, q, cur, actor)
		return err
	})
	return out, err
}

type listedFile struct {
	ID       uuid.UUID `json:"id"`
	Filename string    `json:"filename"`
}

func listFiles(missing []DocFile) []listedFile {
	listed := make([]listedFile, 0, min(len(missing), maxUnopenedListed))
	for _, f := range missing[:min(len(missing), maxUnopenedListed)] {
		listed = append(listed, listedFile{f.ID, f.Filename})
	}
	return listed
}

// errFileNotRead: todos os arquivos foram vistos, mas algum não foi lido até o fim.
func errFileNotRead(missing []DocFile) error {
	return &httpapi.APIError{Status: 422, Code: "file_not_read", Message: "Leia cada arquivo do documento até o fim antes de aprová-lo.",
		Details: map[string]any{"files": listFiles(missing), "total": len(missing)}}
}

func errFileNotOpened(missing []DocFile, decision string) error {
	listed := listFiles(missing)
	msg := "Visualize todos os arquivos do documento antes de aprová-lo."
	if decision == ReviewRejected {
		msg = "Visualize todos os arquivos do documento antes de reprová-lo."
	}
	return &httpapi.APIError{Status: 422, Code: "file_not_opened", Message: msg,
		Details: map[string]any{"files": listed, "total": len(missing)}}
}

// openedBy devolve o que o administrador já fez com cada arquivo do projeto (viu, e se leu até o fim).
func (s *Service) openedBy(ctx context.Context, q *db.Queries, projectID, adminID uuid.UUID) (map[uuid.UUID]fileSeen, error) {
	rows, err := q.ListFileViewsOfAdmin(ctx, db.ListFileViewsOfAdminParams{ProjectID: projectID, AdminID: adminID})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]fileSeen, len(rows))
	for _, r := range rows {
		out[r.FileID] = fileSeen{Opened: true, Read: r.Read}
	}
	return out, nil
}

// recordStaffOpening registra que o administrador viu o arquivo (ou, com read, chegou ao fim dele), se ele é da equipe (nem titular
// nem membro). Quem vê o próprio arquivo não gera registro. Se o registro falhar a chamada falha: melhor do que permitir aprovar sem a prova.
func (s *Service) recordStaffOpening(ctx context.Context, projectID, fileID uuid.UUID, actor Actor, read bool) error {
	if actor.ID == uuid.Nil {
		return nil
	}
	proj, err := s.q.GetProject(ctx, projectID)
	if err != nil {
		return notFound(err)
	}
	access, err := s.accessOf(ctx, s.q, projectID, proj.CreatedByID, actor)
	if err != nil || access != "" {
		return err
	}
	if read {
		return s.q.RecordFileRead(ctx, db.RecordFileReadParams{FileID: fileID, AdminID: actor.ID})
	}
	return s.q.RecordFileView(ctx, db.RecordFileViewParams{FileID: fileID, AdminID: actor.ID})
}

// reviewState monta a conferência a partir do que está gravado. opened pode ser nulo quando só interessa o resultado (allApproved).
func (s *Service) reviewState(ctx context.Context, q *db.Queries, projectID uuid.UUID, cl *Checklist, seen map[uuid.UUID]fileSeen) (Review, error) {
	rows, err := q.ListItemReviews(ctx, projectID)
	if err != nil {
		return Review{}, err
	}
	saved := make(map[string]reviewRow, len(rows))
	for _, x := range rows {
		saved[x.Kind+":"+x.Target] = reviewRow{Status: x.Status, Note: str(x.Note), By: x.ReviewedByName, At: x.ReviewedAt}
	}
	return buildReview(reviewTargets(cl), saved, seen), nil
}

// attachReview acrescenta à visão da equipe o analista responsável e a conferência. O titular e os membros não recebem (é interna),
// nem rascunhos (a equipe não os vê), nem as chamadas internas sem pessoa (Actor vazio).
func (s *Service) attachReview(ctx context.Context, q *db.Queries, r db.Project, p *Project, cl *Checklist, actor Actor) error {
	if p.Access != "" || actor.ID == uuid.Nil || r.Status == StatusDraft {
		return nil
	}
	opened, err := s.openedBy(ctx, q, r.ID, actor.ID)
	if err != nil {
		return err
	}
	rv, err := s.reviewState(ctx, q, r.ID, cl, opened)
	if err != nil {
		return err
	}
	p.Review = &rv
	if p.Framing, p.Framings, err = s.framingViews(ctx, q, r); err != nil {
		return err
	}
	if r.AnalystID != nil {
		a, err := q.GetAdminByID(ctx, *r.AnalystID)
		if err == nil {
			p.Analyst = &AnalystInfo{ID: a.ID, Name: adminDisplayName(a), IsMe: a.ID == actor.ID}
		}
	}
	return nil
}
