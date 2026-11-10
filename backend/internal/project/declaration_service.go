package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mtid"
	"github.com/wagner/convenios-e-fomentos/backend/internal/upload"
)

// Situação de uma declaração emitida.
const (
	DeclAwaiting   = "awaiting_signature"
	DeclSigned     = "signed"
	DeclRejected   = "rejected"
	DeclExpired    = "expired"
	DeclCancelled  = "cancelled"
	DeclSuperseded = "superseded"
)

const pdfType = "application/pdf"

var (
	errSignatureUnavailable = &httpapi.APIError{Status: 503, Code: "signature_unavailable", Message: "A assinatura digital não está disponível agora."}
	errSignatureFailed      = &httpapi.APIError{Status: 502, Code: "signature_failed", Message: "Não foi possível pedir a assinatura ao MT-ID agora. Tente novamente em instantes."}
	errDeclarationChanged   = httpapi.ErrConflict("declaration_changed", "O cadastro mudou depois que você leu a declaração. Leia o texto atualizado e confirme de novo.")
	// errRaceLost interrompe a transação de quem chegou depois de outra conclusão da mesma assinatura.
	errRaceLost = errors.New("declaração já concluída por outra chamada")
)

// SetMTID liga o MT-ID. Sem ele, emitir declaração responde 503 e a consulta de situação só mostra o que já foi registrado.
func (s *Service) SetMTID(c mtid.Client) { s.mtid = c }

// DeclarationRecord é a situação da última declaração emitida para um documento.
type DeclarationRecord struct {
	ID         uuid.UUID `json:"id"`
	Status     string    `json:"status"`
	AcceptedAt time.Time `json:"acceptedAt"`
	// RequestedBySigner indica que quem pediu a emissão é o próprio signatário. Se não for, o pedido de assinatura foi para o
	// signatário, que confirma no MTCidadão: o pedido de outra pessoa não vale como aceite dele.
	RequestedBySigner bool       `json:"requestedBySigner"`
	SignerCPF         string     `json:"signerCpf"` // mascarado
	SignedAt          *time.Time `json:"signedAt"`
	FileID            *uuid.UUID `json:"fileId"`
	// Stale: o cadastro mudou depois da emissão (o resumo do conteúdo não é mais o mesmo). Aguardando ou assinada, ela deixa de valer.
	Stale bool `json:"stale"`
	// StaleReasons diz o que mudou (códigos estáveis, só com Stale): signer_name, signer_role, signer_cpf, text, place, template.
	StaleReasons []string `json:"staleReasons"`
}

// Motivos pelos quais uma declaração emitida deixou de refletir o cadastro (DeclarationRecord.StaleReasons).
const (
	StaleSignerName = "signer_name" // nome de quem assina
	StaleSignerRole = "signer_role" // cargo de quem assina (vai impresso no PDF)
	StaleSignerCPF  = "signer_cpf"  // CPF de quem assina (por exemplo, passou a assinar outro representante)
	StaleText       = "text"        // o texto da declaração mudou (dados do projeto, da instituição ou da pessoa que entram nele)
	StalePlace      = "place"       // município, nome da instituição ou município do endereço de quem assina
	StaleTemplate   = "template"    // o modelo oficial foi atualizado
)

// staleReasons compara o que foi emitido com a declaração montada hoje e diz o que mudou. Só usa o que a emissão guardou (nome, cargo e
// CPF de quem assina, texto e versão do modelo); o que sobrar de diferença no resumo é local ou instituição. Vazio se nada mudou.
func staleReasons(r db.ProjectDeclaration, spec declSpec, d GeneratedDeclaration) []string {
	if r.ParamsSha256 == d.ParamsSHA256 {
		return []string{}
	}
	out := []string{}
	if r.TemplateVersion != spec.version {
		out = append(out, StaleTemplate)
	}
	if r.SignerName != d.SignerName {
		out = append(out, StaleSignerName)
	}
	if r.SignerRole != d.SignerRole {
		out = append(out, StaleSignerRole)
	}
	if r.SignerCpf != d.signerCPF {
		out = append(out, StaleSignerCPF)
	}
	if r.RenderedText != strings.Join(d.Paragraphs, "\n\n") {
		out = append(out, StaleText)
	}
	if len(out) == 0 {
		out = append(out, StalePlace)
	}
	return out
}

// recordView é a situação da declaração emitida frente à declaração montada agora (d, com o texto ainda completo, sem a máscara do CPF).
func recordView(r db.ProjectDeclaration, d GeneratedDeclaration) DeclarationRecord {
	active := r.Status == DeclAwaiting || r.Status == DeclSigned
	v := DeclarationRecord{
		ID: r.ID, Status: r.Status, AcceptedAt: r.AcceptedAt, RequestedBySigner: r.AcceptedByCpf == r.SignerCpf,
		SignerCPF: cpf.Mask(r.SignerCpf), SignedAt: r.SignedAt, FileID: r.FileID, Stale: active && r.ParamsSha256 != d.ParamsSHA256,
		StaleReasons: []string{},
	}
	if v.Stale {
		v.StaleReasons = staleReasons(r, declarationSpecs[r.RequirementCode], d)
	}
	return v
}

// AcceptDeclarationInput é o aceite: o resumo do texto que a pessoa leu e a confirmação de que quer emitir.
type AcceptDeclarationInput struct {
	ParamsSHA256 string `json:"paramsSha256"`
	Confirm      bool   `json:"confirm"`
}

// AcceptDeclaration grava o aceite da declaração, gera o PDF e pede a assinatura ao MT-ID. Só emite o que a pessoa leu: se o
// cadastro mudou desde a prévia, o resumo não confere e responde 409. Uma declaração para cada documento: pedir de novo com o
// mesmo conteúdo devolve o pedido em andamento; com conteúdo diferente, cancela o pedido anterior (ou substitui a declaração
// assinada que ficou desatualizada).
func (s *Service) AcceptDeclaration(ctx context.Context, actor Actor, id uuid.UUID, code string, in AcceptDeclarationInput, upd authz.Decision) (*GeneratedDeclaration, error) {
	spec, ok := declarationSpecs[code]
	if !ok {
		return nil, httpapi.ErrNotFound()
	}
	if s.mtid == nil {
		return nil, errSignatureUnavailable
	}
	if err := s.requireStore(); err != nil {
		return nil, err
	}
	if !in.Confirm {
		return nil, fieldIssue("confirm", "Confirme que leu o texto da declaração.")
	}
	s.settle(ctx, id)

	// 1. Confere permissão, prazo e conteúdo, sem chamar nada de fora.
	var d GeneratedDeclaration
	var projectCode string
	var current *db.ProjectDeclaration
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, _, err := s.lockedItem(ctx, q, id, code, "", upd, actor); err != nil {
			return err
		}
		cur, err := q.GetProject(ctx, id)
		if err != nil {
			return err
		}
		projectCode = str(cur.Code)
		if d, err = s.draftDeclaration(ctx, q, cur, spec, actor); err != nil {
			return err
		}
		if !d.Ready {
			e := httpapi.ErrValidation(map[string]any{"issues": d.Issues})
			e.Code, e.Message = "declaration_not_ready", "Faltam dados no cadastro para emitir esta declaração."
			return e
		}
		if in.ParamsSHA256 != d.ParamsSHA256 {
			return errDeclarationChanged
		}
		rec, err := q.GetActiveProjectDeclaration(ctx, db.GetActiveProjectDeclarationParams{ProjectID: id, RequirementCode: code})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return err
		case rec.ParamsSha256 != d.ParamsSHA256:
			return nil // cadastro mudou desde a emissão: a nova substitui a antiga (passo 3)
		case rec.Status == DeclSigned:
			return httpapi.ErrConflict("declaration_already_signed", "Esta declaração já está assinada.")
		}
		current = &rec // pedido em andamento com o mesmo conteúdo: devolvido como está
		return nil
	})
	if err != nil {
		return nil, err
	}
	if current != nil {
		v := recordView(*current, d)
		d.Current = &v
		d.redact()
		return &d, nil
	}

	// 2. Gera o PDF e pede a assinatura. Nada disso fica dentro de transação: são chamadas de rede.
	pdf, spot, err := renderDeclaration(d, s.now())
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(pdf)
	declID := uuid.New()
	key := fmt.Sprintf("projects/%s/declarations/%s.pdf", id, declID)
	if err := s.store.PutObject(ctx, key, pdfType, pdf); err != nil {
		return nil, err
	}
	fileID, err := s.mtid.Upload(ctx, declFilename(code, false), pdf)
	if err != nil {
		s.dropObject(ctx, key)
		return nil, s.signatureError(ctx, id, code, "upload", err)
	}
	orderID, err := s.mtid.RequestSignature(ctx, mtid.Request{
		SignerCPF: d.signerCPF, Description: d.Name + " — projeto " + projectCode, FileID: fileID,
		X: spot.X, Y: spot.Y, Pages: []int{spot.Page},
		Metadata: map[string]string{"projectId": id.String(), "declaration": code},
	})
	if err != nil {
		s.dropObject(ctx, key)
		return nil, s.signatureError(ctx, id, code, "request", err)
	}

	// 3. Registra. Se o registro falhar, desfaz o pedido e o objeto.
	var toCancel string
	var rec db.ProjectDeclaration
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		old, err := q.GetActiveProjectDeclarationForUpdate(ctx, db.GetActiveProjectDeclarationForUpdateParams{ProjectID: id, RequirementCode: code})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return err
		case old.ParamsSha256 == d.ParamsSHA256:
			return httpapi.ErrConflict("declaration_in_progress", "Esta declaração já foi emitida por outra chamada. Recarregue a página.")
		case old.Status == DeclAwaiting:
			toCancel = old.MtidOrderID
			if _, err := q.SetProjectDeclarationStatus(ctx, db.SetProjectDeclarationStatusParams{ID: old.ID, FromStatus: DeclAwaiting, Status: DeclCancelled}); err != nil {
				return err
			}
		case old.Status == DeclSigned:
			// A assinada ficou desatualizada: sai do checklist até a nova ser assinada.
			if _, err := q.SetProjectDeclarationStatus(ctx, db.SetProjectDeclarationStatusParams{ID: old.ID, FromStatus: DeclSigned, Status: DeclSuperseded}); err != nil {
				return err
			}
			if old.FileID != nil {
				if _, err := q.SupersedeProjectFile(ctx, *old.FileID); err != nil {
					return err
				}
			}
		}
		cur, err := q.GetProject(ctx, id)
		if err != nil {
			return err
		}
		rec, err = q.InsertProjectDeclaration(ctx, db.InsertProjectDeclarationParams{
			ID: declID, ProjectID: id, RequirementCode: code, ProjectVersion: cur.Version, TemplateVersion: spec.version,
			ParamsSha256: d.ParamsSHA256, RenderedText: strings.Join(d.Paragraphs, "\n\n"), SignerName: d.SignerName, SignerRole: d.SignerRole,
			SignerCpf: d.signerCPF, AcceptedByID: &actor.ID, AcceptedByCpf: actor.CPF, AcceptedIp: actor.IP,
			PdfObjectKey: key, PdfSha256: hex.EncodeToString(sum[:]), MtidFileID: fileID, MtidOrderID: orderID,
		})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("project.declaration-request", id, map[string]any{
			"declaration": code, "id": declID, "paramsSha256": d.ParamsSHA256, "templateVersion": spec.version,
			"signerCpf": cpf.Mask(d.signerCPF), "requestedBySigner": actor.CPF == d.signerCPF, "orderId": orderID,
		}))
	})
	if err != nil {
		s.dropObject(ctx, key)
		if cerr := s.mtid.Cancel(ctx, orderID); cerr != nil && s.log != nil {
			s.log.ErrorContext(ctx, "cancelando pedido de assinatura sem registro", "project", id, "order", orderID, "error", cerr)
		}
		return nil, err
	}
	if toCancel != "" {
		if err := s.mtid.Cancel(ctx, toCancel); err != nil && s.log != nil {
			s.log.WarnContext(ctx, "cancelando pedido de assinatura substituído", "project", id, "order", toCancel, "error", err)
		}
	}
	v := recordView(rec, d)
	d.Current = &v
	d.redact()
	return &d, nil
}

// CancelDeclaration cancela o pedido de assinatura em andamento (a pessoa desistiu ou quer corrigir o cadastro antes).
func (s *Service) CancelDeclaration(ctx context.Context, actor Actor, id uuid.UUID, code string, upd authz.Decision) error {
	if _, ok := declarationSpecs[code]; !ok {
		return httpapi.ErrNotFound()
	}
	if s.mtid == nil {
		return errSignatureUnavailable
	}
	var order string
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, _, err := s.lockedItem(ctx, q, id, code, "", upd, actor); err != nil {
			return err
		}
		rec, err := q.GetActiveProjectDeclarationForUpdate(ctx, db.GetActiveProjectDeclarationForUpdateParams{ProjectID: id, RequirementCode: code})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.ErrConflict("declaration_not_pending", "Não há pedido de assinatura em andamento para esta declaração.")
			}
			return err
		}
		if rec.Status != DeclAwaiting {
			return httpapi.ErrConflict("declaration_not_pending", "Não há pedido de assinatura em andamento para esta declaração.")
		}
		order = rec.MtidOrderID
		if _, err := q.SetProjectDeclarationStatus(ctx, db.SetProjectDeclarationStatusParams{ID: rec.ID, FromStatus: DeclAwaiting, Status: DeclCancelled}); err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("project.declaration-cancel", id, map[string]any{"declaration": code, "id": rec.ID}))
	})
	if err != nil {
		return err
	}
	if err := s.mtid.Cancel(ctx, order); err != nil && !errors.Is(err, mtid.ErrNotFound) && s.log != nil {
		s.log.WarnContext(ctx, "cancelando pedido de assinatura", "project", id, "order", order, "error", err)
	}
	return nil
}

// refreshDeclaration consulta o MT-ID sobre um pedido pendente e registra o resultado: assinada (baixa o arquivo e o põe no
// checklist), recusada ou vencida. Falha do MT-ID não é erro de quem consulta: o pedido continua pendente e vale a próxima consulta.
func (s *Service) refreshDeclaration(ctx context.Context, rec db.ProjectDeclaration) db.ProjectDeclaration {
	if s.mtid == nil || s.store == nil || rec.Status != DeclAwaiting {
		return rec
	}
	st, err := s.mtid.Status(ctx, rec.MtidOrderID)
	if err != nil {
		s.warn(ctx, "consultando pedido de assinatura", rec, err)
		return rec
	}
	switch st.State {
	case mtid.StateApproved:
		if err := s.completeDeclaration(ctx, rec, st); err != nil {
			s.warn(ctx, "concluindo assinatura da declaração", rec, err)
			return rec
		}
	case mtid.StateRejected, mtid.StateExpired:
		next := map[mtid.State]string{mtid.StateRejected: DeclRejected, mtid.StateExpired: DeclExpired}[st.State]
		err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
			q := s.q.WithTx(tx)
			n, err := q.SetProjectDeclarationStatus(ctx, db.SetProjectDeclarationStatusParams{ID: rec.ID, FromStatus: DeclAwaiting, Status: next})
			if err != nil || n == 0 {
				return err
			}
			return s.audit.Record(ctx, q, auditSystem("project.declaration-"+next, rec.ProjectID, map[string]any{"declaration": rec.RequirementCode, "id": rec.ID}))
		})
		if err != nil {
			s.warn(ctx, "registrando fim do pedido de assinatura", rec, err)
			return rec
		}
	default:
		return rec
	}
	fresh, err := s.q.GetProjectDeclaration(ctx, rec.ID)
	if err != nil {
		return rec
	}
	return fresh
}

// settleDeclarations conclui as assinaturas pendentes do projeto: consulta o MT-ID sobre cada declaração que aguarda assinatura e, se
// já foi assinada (ou recusada, ou venceu), registra o resultado e põe o arquivo no checklist. Quem lê o projeto, o checklist ou
// envia o projeto vê o que o signatário já assinou sem precisar abrir a declaração. Chamada fora de transação (fala com a rede); a
// falha do MT-ID não é erro de quem lê: o pedido segue pendente e vale a próxima leitura.
func (s *Service) settleDeclarations(ctx context.Context, id uuid.UUID) {
	if s.mtid == nil || s.store == nil {
		return
	}
	recs, err := s.q.ListAwaitingProjectDeclarations(ctx, id)
	if err != nil {
		if s.log != nil {
			s.log.WarnContext(ctx, "listando assinaturas pendentes", "project", id, "error", err)
		}
		return
	}
	for _, rec := range recs {
		s.refreshDeclaration(ctx, rec)
	}
}

// settleDeclarationsFor é settleDeclarations para as ações de gravação: só consulta o MT-ID se a decisão cobre o projeto.
func (s *Service) settleDeclarationsFor(ctx context.Context, id uuid.UUID, dec authz.Decision) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil || !dec.AllowsRecord(refOf(r).Getter()) {
		return
	}
	s.settleDeclarations(ctx, id)
}

func (s *Service) warn(ctx context.Context, msg string, rec db.ProjectDeclaration, err error) {
	if s.log != nil {
		s.log.WarnContext(ctx, msg, "project", rec.ProjectID, "declaration", rec.RequirementCode, "order", rec.MtidOrderID, "error", err)
	}
}

// completeDeclaration traz o PDF assinado, confere (formato, tamanho, vírus) e o põe no item do checklist como arquivo confirmado.
func (s *Service) completeDeclaration(ctx context.Context, rec db.ProjectDeclaration, st mtid.Status) error {
	data, err := s.mtid.Download(ctx, st.FileID)
	if err != nil {
		return err
	}
	rule := upload.Rules[pdfType]
	if len(data) == 0 || int64(len(data)) > rule.MaxSize || !rule.Magic(data) {
		return fmt.Errorf("arquivo assinado inválido (%d bytes)", len(data))
	}
	fileID := uuid.New()
	key := fmt.Sprintf("projects/%s/%s", rec.ProjectID, fileID)
	if err := s.store.PutObject(ctx, key, pdfType, data); err != nil {
		return err
	}
	sum, infected, err := s.verifyContent(ctx, key, int64(len(data)))
	if infected != nil {
		s.dropObject(ctx, key)
		_ = s.audit.Record(ctx, s.q, auditSystem("project.file-infected", rec.ProjectID, map[string]any{"declaration": rec.RequirementCode, "signature": infected.Signature}))
		return errors.New("o arquivo assinado foi recusado pela verificação de segurança")
	}
	if err != nil {
		s.dropObject(ctx, key)
		return err
	}
	signedAt := st.SignedAt
	if signedAt.IsZero() {
		signedAt = s.now()
	}
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		item, err := q.GetProjectDocumentItem(ctx, db.GetProjectDocumentItemParams{ProjectID: rec.ProjectID, RequirementCode: &rec.RequirementCode})
		if errors.Is(err, pgx.ErrNoRows) {
			item, err = q.CreateProjectDocumentItem(ctx, db.CreateProjectDocumentItemParams{ProjectID: rec.ProjectID, RequirementCode: &rec.RequirementCode})
		}
		if err != nil {
			return err
		}
		if _, err := q.InsertProjectFile(ctx, db.InsertProjectFileParams{ID: fileID, ItemID: item.ID, ObjectKey: key, Filename: declFilename(rec.RequirementCode, true),
			ContentType: pdfType, SizeBytes: int64(len(data)), CreatedByID: rec.AcceptedByID}); err != nil {
			return err
		}
		if _, err := q.ConfirmProjectFile(ctx, db.ConfirmProjectFileParams{ID: fileID, SizeBytes: int64(len(data)), Sha256: &sum}); err != nil {
			return err
		}
		n, err := q.SignProjectDeclaration(ctx, db.SignProjectDeclarationParams{ID: rec.ID, SignedAt: &signedAt, FileID: &fileID})
		if err != nil {
			return err
		}
		if n == 0 {
			return errRaceLost
		}
		return s.audit.Record(ctx, q, auditSystem("project.declaration-signed", rec.ProjectID, map[string]any{
			"declaration": rec.RequirementCode, "id": rec.ID, "file": fileID, "sha256": sum, "signerCpf": cpf.Mask(rec.SignerCpf),
		}))
	})
	if err != nil {
		s.dropObject(ctx, key)
		if errors.Is(err, errRaceLost) {
			return nil
		}
		return err
	}
	return nil
}

// dropObject apaga um objeto que não chegou a ser registrado; falha só é registrada no log.
func (s *Service) dropObject(ctx context.Context, key string) {
	if err := s.store.Delete(ctx, key); err != nil && s.log != nil {
		s.log.WarnContext(ctx, "apagando objeto sem registro", "key", key, "error", err)
	}
}

func (s *Service) signatureError(ctx context.Context, id uuid.UUID, code, step string, err error) error {
	if s.log != nil {
		s.log.ErrorContext(ctx, "MT-ID indisponível", "project", id, "declaration", code, "step", step, "error", err)
	}
	return errSignatureFailed
}

// declFilename é o nome do arquivo da declaração: só ASCII, para o nome sobreviver a qualquer cabeçalho.
func declFilename(code string, signed bool) string {
	n := strings.ReplaceAll(code, "_", "-")
	if signed {
		return n + "-assinada.pdf"
	}
	return n + ".pdf"
}

// markStaleDeclarations tira do checklist a declaração assinada que ficou desatualizada: o cadastro mudou depois da assinatura,
// então ela não vale mais e precisa ser emitida de novo. O item volta a faltar (ou a ficar justificado) e fica marcado.
func (s *Service) markStaleDeclarations(ctx context.Context, q *db.Queries, r db.Project, p *Project, cl *Checklist) error {
	signed := map[string]db.ProjectDeclaration{}
	recs, err := q.ListProjectDeclarations(ctx, r.ID)
	if err != nil {
		return err
	}
	awaiting := map[string]bool{}
	for _, rec := range recs {
		switch rec.Status {
		case DeclSigned:
			signed[rec.RequirementCode] = rec
		case DeclAwaiting:
			awaiting[rec.RequirementCode] = true
		}
	}
	for i := range cl.Items {
		cl.Items[i].DeclarationAwaiting = awaiting[cl.Items[i].Code]
		cl.Items[i].DeclarationStaleReasons = []string{}
	}
	if len(signed) == 0 {
		return nil
	}
	env, err := declarationEnv(ctx, q, r, *p)
	if err != nil {
		return err
	}
	for i := range cl.Items {
		it := &cl.Items[i]
		rec, ok := signed[it.Code]
		spec, hasSpec := declarationSpecs[it.Code]
		if !ok || !hasSpec {
			continue
		}
		now := buildDeclaration(spec, *p, env)
		if now.ParamsSHA256 == rec.ParamsSha256 {
			continue
		}
		it.DeclarationStale = true
		it.DeclarationStaleReasons = staleReasons(rec, spec, now)
		if it.Status != ItemDone || hasOtherConfirmedFile(*it, rec) {
			continue
		}
		if it.Justification != "" && it.AcceptsJustification {
			it.Status = ItemJustified
		} else {
			it.Status = ItemMissing
		}
	}
	cl.tally()
	return nil
}

// hasOtherConfirmedFile diz se o item tem algum arquivo confirmado além do da declaração assinada (que mantém o documento entregue).
func hasOtherConfirmedFile(it ChecklistItem, rec db.ProjectDeclaration) bool {
	for _, f := range it.Files {
		if f.Status == FileConfirmed && (rec.FileID == nil || f.ID != *rec.FileID) {
			return true
		}
	}
	return false
}
