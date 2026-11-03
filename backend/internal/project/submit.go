package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
)

// DeclarationText é a declaração de veracidade que o proponente aceita ao enviar.
const DeclarationText = "Declaro, sob as penas da lei, que as informações e os documentos apresentados neste projeto são verdadeiros e que respondo por eles."

// Motivos de uma versão.
const ReasonInitial = "initial"

// Declaration registra o aceite da declaração de veracidade.
type Declaration struct {
	Text       string    `json:"text"`
	AcceptedAt time.Time `json:"acceptedAt"`
	ByName     string    `json:"byName"`
	ByCPF      string    `json:"byCpf"` // mascarado
	IP         string    `json:"ip"`
}

// VersionSnapshot é o retrato imutável do projeto no momento do envio.
type VersionSnapshot struct {
	Project     Project        `json:"project"`
	Documents   Checklist      `json:"documents"`
	Rules       rules.Snapshot `json:"rules"`
	Declaration Declaration    `json:"declaration"`
}

// SubmitInput é o corpo do envio: a versão que o proponente viu e o aceite da declaração.
type SubmitInput struct {
	Version     int32 `json:"version" validate:"required,min=1"`
	Declaration bool  `json:"declaration"`
}

// SetNotifier informa o endereço público do painel, usado nos links dos e-mails. Os e-mails em si são gravados na caixa de saída
// (notify.go) e enviados por outro processo.
func (s *Service) SetNotifier(publicURL string, log *slog.Logger) {
	s.publicURL, s.log = strings.TrimRight(publicURL, "/"), log
}

func brDate(iso string) string {
	if t, err := time.Parse(DateLayout, iso); err == nil {
		return t.Format("02/01/2006")
	}
	return iso
}

// deadlineIssue avisa quando a data-limite de protocolo do ano eleitoral já passou (Portaria Conjunta 71/2026).
// Não bloqueia: o projeto é recebido, mas só tem atos preparatórios até o fim da vedação.
func (s *Service) deadlineIssue(snap *rules.Snapshot) *Issue {
	if snap == nil || snap.ElectoralLimit == "" {
		return nil
	}
	limit, err := time.Parse(DateLayout, snap.ElectoralLimit)
	if err != nil || !s.today().After(limit) {
		return nil
	}
	return &Issue{Section: "submission", Field: "electoralDeadline", Code: "electoral_deadline_passed", Severity: SeverityWarn,
		Message: fmt.Sprintf("A data-limite de protocolo antes da vedação eleitoral (%s) já passou. O projeto pode ser enviado, mas só terá atos preparatórios e preliminares: o repasse fica para depois do período de restrição.", brDate(snap.ElectoralLimit))}
}

func errHasIssues(issues []Issue) error {
	e := httpapi.ErrValidation(map[string]any{"issues": issues})
	e.Code = "project_has_issues"
	e.Message = "O projeto ainda tem pendências que impedem o envio."
	return e
}

// Submit envia o rascunho: confere as pendências, a versão lida e a declaração; dá o código ao projeto; guarda o
// retrato imutável (versão 1) e confirma o recebimento por e-mail. Depois disso o projeto não é mais editável pelo proponente.
func (s *Service) Submit(ctx context.Context, actor Actor, id uuid.UUID, in SubmitInput, sub authz.Decision) (*Project, error) {
	if !in.Declaration {
		return nil, fieldIssue("declaration", "É preciso aceitar a declaração de veracidade para enviar o projeto.")
	}
	var (
		out     *Project
		pending []string
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
		if cur.Status != StatusDraft {
			return httpapi.ErrConflict("project_already_submitted", "Este projeto já foi enviado.")
		}
		if cur.Version != in.Version {
			return errVersionConflict(cur.Version)
		}
		draft, _, err := s.viewWithChecklist(ctx, q, cur, actor)
		if err != nil {
			return err
		}
		var blocks []Issue
		for _, i := range draft.Issues {
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
		late := s.deadlineIssue(snap) != nil

		// Envios de arquivo nunca confirmados não entram no que foi enviado.
		if pending, err = q.DeleteProjectPendingFiles(ctx, id); err != nil {
			return err
		}

		if err := q.LockProjectFiles(ctx, id); err != nil {
			return err
		}
		n, err := q.NextProjectCodeNumber(ctx, int32(s.today().Year())) //nolint:gosec // ano
		if err != nil {
			return err
		}
		code := fmt.Sprintf("%d-%06d", s.today().Year(), n)
		row, err := q.SubmitProject(ctx, db.SubmitProjectParams{ID: id, Code: &code, SubmittedByID: &actor.ID, SubmissionIp: actor.IP, LateSubmission: late})
		if err != nil {
			if database.IsUniqueViolation(err) {
				return fieldIssue("sigconNumber", "Já existe um projeto enviado com este número de proposta do SIGCon.")
			}
			return notFound(err)
		}
		final, cl, err := s.viewWithChecklist(ctx, q, row, actor)
		if err != nil {
			return err
		}
		decl := Declaration{Text: DeclarationText, AcceptedAt: *row.SubmittedAt, ByName: actor.Name, ByCPF: cpf.Mask(actor.CPF), IP: actor.IP}
		raw, err := json.Marshal(VersionSnapshot{Project: *final, Documents: *cl, Rules: *snap, Declaration: decl})
		if err != nil {
			return err
		}
		if _, err := q.InsertProjectVersion(ctx, db.InsertProjectVersionParams{ProjectID: id, Number: 1, Reason: ReasonInitial, SubmittedAt: *row.SubmittedAt,
			SubmittedByID: &actor.ID, SubmissionIp: actor.IP, Snapshot: raw}); err != nil {
			return err
		}
		if err := s.transition(ctx, q, row, actor.entry("project.submit", id, map[string]any{"code": code, "late": late, "version": 1})); err != nil {
			return err
		}
		if err := s.notifySubmitted(ctx, q, final); err != nil {
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

func cuiaba() *time.Location {
	loc, err := time.LoadLocation("America/Cuiaba")
	if err != nil {
		return time.FixedZone("-04", -4*3600)
	}
	return loc
}

// Receipt devolve o comprovante de recebimento (PDF) do projeto enviado e o nome sugerido para o arquivo. É gerado a partir
// do retrato imutável do envio, então sempre sai igual. 404 se o projeto não está ao alcance da leitura ou ainda é rascunho.
func (s *Service) Receipt(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) ([]byte, string, error) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, "", notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) || r.Status == StatusDraft {
		return nil, "", httpapi.ErrNotFound()
	}
	v, err := s.q.GetProjectVersion(ctx, db.GetProjectVersionParams{ProjectID: id, Number: 1})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", httpapi.ErrNotFound()
	}
	if err != nil {
		return nil, "", err
	}
	var snap VersionSnapshot
	if err := json.Unmarshal(v.Snapshot, &snap); err != nil {
		return nil, "", err
	}
	pdf, err := renderReceipt(snap)
	if err != nil {
		return nil, "", err
	}
	if err := s.audit.Record(ctx, s.q, actor.entry("project.receipt-download", id, map[string]any{"version": 1})); err != nil {
		return nil, "", err
	}
	return pdf, "comprovante-" + snap.Project.Code + ".pdf", nil
}

// auditSystem é a entrada de auditoria de uma ação do próprio sistema (sem pessoa).
func auditSystem(action string, id uuid.UUID, payload map[string]any) audit.Entry {
	return audit.Entry{ActorType: "system", Action: action, Entity: "project", EntityID: id.String(), Payload: payload}
}
