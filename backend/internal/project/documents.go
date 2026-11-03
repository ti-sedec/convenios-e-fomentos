package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/scan"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
	"github.com/wagner/convenios-e-fomentos/backend/internal/upload"
)

// Situação de um item do checklist e de um arquivo.
const (
	ItemMissing   = "missing"
	ItemDone      = "done"
	ItemJustified = "justified"

	FilePending   = "pending"
	FileConfirmed = "confirmed"
)

const (
	maxFilesPerItem = 10
	// Documentos adicionais: itens livres do proponente, sem exigência do catálogo e que nunca bloqueiam o envio.
	maxExtraDocuments   = 5
	maxExtraTitle       = 120
	maxExtraDescription = 1000
	maxJustification    = 2000
	pendingGraceHours   = 24

	// Limites de armazenamento por projeto: protegem contra abuso (envios sem fim) sem atrapalhar um projeto real, que tem dezenas de
	// documentos. Contam todos os arquivos guardados, inclusive os substituídos, e os tamanhos declarados no pedido de envio.
	maxFilesPerProject   = 300
	maxPendingPerProject = 20
	maxBytesPerProject   = int64(2) << 30
)

// DocumentOptions são as situações que acrescentam documentos ao checklist.
type DocumentOptions struct {
	ExclusiveSupplier bool `json:"exclusiveSupplier"` // contratação por exclusividade
	PublicSpaceUse    bool `json:"publicSpaceUse"`    // atividade em espaço público
	ReducedDeadline   bool `json:"reducedDeadline"`   // pedido de redução do prazo de tramitação
}

// DocumentSummary resume o checklist. Os campos Required* contam só as exigências obrigatórias: são eles que bloqueiam a
// continuidade e o envio. Um obrigatório justificado conta como cumprido.
type DocumentSummary struct {
	Total           int `json:"total"`
	Done            int `json:"done"`
	Justified       int `json:"justified"`
	Missing         int `json:"missing"`
	RequiredTotal   int `json:"requiredTotal"`
	RequiredMissing int `json:"requiredMissing"`
}

// DocFile é um arquivo anexado a um item. A chave do objeto nunca sai da API.
type DocFile struct {
	ID          uuid.UUID `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	Status      string    `json:"status"`
	// SHA256 é o hash do conteúdo, calculado na confirmação. Vazio nos arquivos anteriores ao recurso e nos ainda não confirmados.
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"createdAt"`
}

// ChecklistItem é uma exigência aplicável ao projeto, com o que já foi anexado.
type ChecklistItem struct {
	Code                 string    `json:"code"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	Group                string    `json:"group"`
	Scope                string    `json:"scope"`
	AmendmentID          string    `json:"amendmentId"`
	AmendmentLabel       string    `json:"amendmentLabel"`
	Source               string    `json:"source"`
	Template             *Template `json:"template"`
	AcceptsJustification bool      `json:"acceptsJustification"`
	// Mandatory: sem arquivo nem justificativa, o documento bloqueia a continuidade e o envio. Falso = opcional.
	Mandatory     bool      `json:"mandatory"`
	Files         []DocFile `json:"files"`
	Justification string    `json:"justification"`
	Status        string    `json:"status"`
	// DeclarationStale: a declaração assinada deste item ficou desatualizada porque o cadastro mudou; emita de novo.
	DeclarationStale bool `json:"declarationStale"`
	// DeclarationAwaiting: a declaração foi emitida e aguarda a assinatura; não há mais nada a fazer pelo proponente.
	DeclarationAwaiting bool `json:"declarationAwaiting"`
}

// ExtraDocument é um documento adicional: o proponente dá o título e a descrição e anexa um único arquivo. Não conta no resumo.
type ExtraDocument struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	File        *DocFile  `json:"file"`
	// Locked: o arquivo já fez parte de uma versão enviada; o documento não pode mais ser alterado nem removido.
	Locked bool `json:"locked"`
}

// Checklist é a lista de documentos do projeto.
type Checklist struct {
	Options        DocumentOptions `json:"options"`
	Instrument     string          `json:"instrument"`
	Summary        DocumentSummary `json:"summary"`
	Items          []ChecklistItem `json:"items"`
	ExtraDocuments []ExtraDocument `json:"extraDocuments"`
}

// requirement é uma linha do catálogo já interpretada.
type requirement struct {
	row         db.DocumentRequirement
	conds       []Clause
	sources     map[string]string
	templates   map[string]Template
	instruments map[string]bool
}

func (s *Service) requirements(ctx context.Context, q *db.Queries) ([]requirement, error) {
	rows, err := q.ListDocumentRequirements(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]requirement, 0, len(rows))
	for _, r := range rows {
		x := requirement{row: r, instruments: map[string]bool{}}
		for _, i := range r.Instruments {
			x.instruments[i] = true
		}
		if err := unmarshalJSON(r.Conditions, &x.conds); err != nil {
			return nil, fmt.Errorf("condições da exigência %q: %w", r.Code, err)
		}
		_ = unmarshalJSON(r.Sources, &x.sources)
		_ = unmarshalJSON(r.Templates, &x.templates)
		out = append(out, x)
	}
	return out, nil
}

// applies informa se a exigência vale para o projeto.
func (r requirement) applies(f map[string]string) bool {
	return r.instruments[f["instrument"]] && holds(r.conds, f)
}

func amendmentLabel(a Amendment) string {
	who := a.ParliamentarianName
	if who == "" {
		who = "parlamentar a informar"
	}
	num := a.Number
	if num == "" {
		num = "sem número"
	}
	if a.Year != 0 {
		num = fmt.Sprintf("%s/%d", num, a.Year)
	}
	return fmt.Sprintf("Emenda %s — %s", num, who)
}

// checklist monta a lista de documentos do projeto: as exigências aplicáveis às respostas dele (uma por emenda nas exigências
// por emenda), com os arquivos e a justificativa já registrados.
func (s *Service) checklist(ctx context.Context, q *db.Queries, p Project) (*Checklist, error) {
	cl := &Checklist{Options: p.DocumentOptions, Instrument: p.Institution.Instrument, Items: []ChecklistItem{}, ExtraDocuments: []ExtraDocument{}}
	reqs, err := s.requirements(ctx, q)
	if err != nil {
		return nil, err
	}
	items, err := q.ListProjectDocumentItems(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	files, err := q.ListProjectFiles(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	type key struct{ code, amendment string }
	byKey := map[key]db.ProjectDocumentItem{}
	for _, it := range items {
		if it.RequirementCode != nil {
			byKey[key{*it.RequirementCode, uuidString(it.AmendmentID)}] = it
		}
	}
	filesByItem := map[uuid.UUID][]DocFile{}
	lockedItems := map[uuid.UUID]bool{}
	for _, f := range files {
		filesByItem[f.ItemID] = append(filesByItem[f.ItemID], DocFile{ID: f.ID, Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.SizeBytes, Status: f.Status, SHA256: str(f.Sha256), CreatedAt: f.CreatedAt})
		if f.Locked {
			lockedItems[f.ItemID] = true
		}
	}
	for _, it := range items {
		if it.RequirementCode != nil {
			continue
		}
		x := ExtraDocument{ID: it.ID, Title: it.Title, Description: it.Description, Locked: lockedItems[it.ID]}
		if fs := filesByItem[it.ID]; len(fs) > 0 {
			x.File = &fs[0]
		}
		cl.ExtraDocuments = append(cl.ExtraDocuments, x)
	}
	if p.Institution.Instrument == "" {
		return cl, nil
	}

	f := facts(p)
	add := func(r requirement, amendment *Amendment) {
		it := ChecklistItem{Code: r.row.Code, Name: r.row.Name, Description: r.row.Description, Group: r.row.Grp, Scope: r.row.Scope,
			Source: r.sources[p.Institution.Instrument], AcceptsJustification: r.row.AcceptsJustification, Mandatory: r.row.Mandatory, Files: []DocFile{}}
		if t, ok := r.templates[p.Institution.Instrument]; ok {
			it.Template = &t
		}
		k := key{code: r.row.Code}
		if amendment != nil {
			it.AmendmentID, it.AmendmentLabel = amendment.ID, amendmentLabel(*amendment)
			k.amendment = amendment.ID
		}
		if row, ok := byKey[k]; ok {
			it.Justification = row.Justification
			if fs := filesByItem[row.ID]; fs != nil {
				it.Files = fs
			}
		}
		it.Status = ItemMissing
		for _, fl := range it.Files {
			if fl.Status == FileConfirmed {
				it.Status = ItemDone
			}
		}
		if it.Status == ItemMissing && it.Justification != "" && it.AcceptsJustification {
			it.Status = ItemJustified
		}
		cl.Items = append(cl.Items, it)
	}
	for _, r := range reqs {
		if !r.applies(f) {
			continue
		}
		if r.row.Scope == ScopeAmendment {
			for i := range p.Funding.Amendments {
				add(r, &p.Funding.Amendments[i])
			}
			continue
		}
		add(r, nil)
	}
	cl.tally()
	return cl, nil
}

// tally recalcula o resumo a partir dos itens. Deve ser chamado sempre que a situação de um item muda.
func (cl *Checklist) tally() {
	cl.Summary = DocumentSummary{}
	for _, it := range cl.Items {
		cl.Summary.Total++
		if it.Mandatory {
			cl.Summary.RequiredTotal++
		}
		switch it.Status {
		case ItemDone:
			cl.Summary.Done++
		case ItemJustified:
			cl.Summary.Justified++
		default:
			cl.Summary.Missing++
			if it.Mandatory {
				cl.Summary.RequiredMissing++
			}
		}
	}
}

// documentIssue bloqueia a continuidade e o envio enquanto faltar documento obrigatório. Opcional pendente não gera pendência.
func documentIssue(sum DocumentSummary) *Issue {
	if sum.RequiredMissing == 0 {
		return nil
	}
	return &Issue{Section: "documents", Field: "documents", Code: "documents_missing", Severity: SeverityBlock,
		Message: fmt.Sprintf("Faltam %d de %d documentos obrigatórios: anexe o arquivo ou justifique o não envio.", sum.RequiredMissing, sum.RequiredTotal)}
}

// ---- Gravação ----

// SaveDocumentOptions grava as situações que acrescentam documentos ao checklist.
func (s *Service) SaveDocumentOptions(ctx context.Context, actor Actor, id uuid.UUID, version int32, in DocumentOptions, upd authz.Decision) (*Project, error) {
	return s.mutate(ctx, actor, id, version, upd, "documents", func(q *db.Queries, _ db.Project) (db.Project, error) {
		return q.UpdateProjectDocumentOptions(ctx, db.UpdateProjectDocumentOptionsParams{ID: id, ExclusiveSupplier: in.ExclusiveSupplier,
			PublicSpaceUse: in.PublicSpaceUse, ReducedDeadline: in.ReducedDeadline, UpdatedByID: &actor.ID})
	})
}

// Checklist devolve a lista de documentos do projeto (404 se a decisão de leitura não cobre o projeto).
func (s *Service) Checklist(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) (*Checklist, error) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) {
		return nil, httpapi.ErrNotFound()
	}
	_, cl, err := s.viewWithChecklist(ctx, s.q, r, actor)
	return cl, err
}

var errStorageUnavailable = &httpapi.APIError{Status: 503, Code: "storage_unavailable", Message: "O armazenamento de arquivos não está configurado."}

// SetStorage liga o armazenamento de arquivos. Sem ele, as rotas de arquivos respondem 503.
func (s *Service) SetStorage(store storage.Store, uploadTTL, downloadTTL time.Duration, log *slog.Logger) {
	s.store, s.uploadTTL, s.downloadTTL, s.log = store, uploadTTL, downloadTTL, log
}

func (s *Service) requireStore() error {
	if s.store == nil {
		return errStorageUnavailable
	}
	return nil
}

// FileInput descreve o arquivo que o cliente pretende enviar.
type FileInput struct {
	Filename    string `json:"filename" validate:"required"`
	ContentType string `json:"contentType" validate:"required"`
	SizeBytes   int64  `json:"sizeBytes" validate:"required"`
	// AmendmentID é obrigatório nas exigências por emenda e proibido nas demais.
	AmendmentID string `json:"amendmentId"`
}

func fieldIssue(field, msg string) error {
	return httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: field, Message: msg}}})
}

// key identifica o item no checklist e nos pedidos de complementação: o código, mais "@" e a emenda nas exigências por emenda.
func (it ChecklistItem) key() string {
	if it.AmendmentID == "" {
		return it.Code
	}
	return it.Code + "@" + it.AmendmentID
}

func missingIn(cl *Checklist, key string) bool {
	for _, it := range cl.Items {
		if it.key() == key {
			return it.Status == ItemMissing
		}
	}
	return false
}

// lockedItem trava o projeto, confere permissão e se o proponente pode mexer no documento agora e acha a exigência (aplicável)
// a que o pedido se refere. Cria o item (espaço do documento) se ainda não existir.
func (s *Service) lockedItem(ctx context.Context, q *db.Queries, id uuid.UUID, code, amendmentID string, upd authz.Decision, actor Actor) (db.ProjectDocumentItem, requirement, error) {
	cur, err := q.GetProjectForUpdate(ctx, id)
	if err != nil {
		return db.ProjectDocumentItem{}, requirement{}, notFound(err)
	}
	if !upd.AllowsRecord(refOf(cur).Getter()) {
		return db.ProjectDocumentItem{}, requirement{}, httpapi.ErrForbidden()
	}
	rt, err := s.rightsOf(ctx, q, cur)
	if err != nil {
		return db.ProjectDocumentItem{}, requirement{}, err
	}
	if !rt.editable() {
		return db.ProjectDocumentItem{}, requirement{}, errNotEditable()
	}
	view, cl, err := s.viewWithChecklist(ctx, q, cur, actor)
	if err != nil {
		return db.ProjectDocumentItem{}, requirement{}, err
	}
	reqs, err := s.requirements(ctx, q)
	if err != nil {
		return db.ProjectDocumentItem{}, requirement{}, err
	}
	var req *requirement
	for i := range reqs {
		if reqs[i].row.Code == code {
			req = &reqs[i]
		}
	}
	if req == nil || !req.applies(facts(*view)) {
		return db.ProjectDocumentItem{}, requirement{}, httpapi.ErrConflict("document_not_applicable", "Este documento não se aplica ao projeto com as respostas atuais.")
	}
	var amendment *uuid.UUID
	switch {
	case req.row.Scope == ScopeAmendment:
		a, err := uuid.Parse(amendmentID)
		found := false
		for _, x := range view.Funding.Amendments {
			found = found || (err == nil && x.ID == a.String())
		}
		if !found {
			return db.ProjectDocumentItem{}, requirement{}, fieldIssue("amendmentId", "Informe uma emenda deste projeto.")
		}
		amendment = &a
	case amendmentID != "":
		return db.ProjectDocumentItem{}, requirement{}, fieldIssue("amendmentId", "Este documento não é por emenda.")
	}
	key := docKey(code, amendment)
	if !rt.canDocument(key, missingIn(cl, key)) {
		return db.ProjectDocumentItem{}, requirement{}, errNotReturned()
	}
	item, err := q.GetProjectDocumentItem(ctx, db.GetProjectDocumentItemParams{ProjectID: id, RequirementCode: &code, AmendmentID: amendment})
	if errors.Is(err, pgx.ErrNoRows) {
		item, err = q.CreateProjectDocumentItem(ctx, db.CreateProjectDocumentItemParams{ProjectID: id, RequirementCode: &code, AmendmentID: amendment})
	}
	if err != nil {
		return db.ProjectDocumentItem{}, requirement{}, err
	}
	return item, *req, s.markAnswered(ctx, q, id, cur, "document", key)
}

// fileRights confere se o proponente pode mexer no documento a que o arquivo pertence agora.
func (s *Service) fileRights(ctx context.Context, id uuid.UUID, f db.GetProjectFileRow) error {
	cur, err := s.q.GetProject(ctx, id)
	if err != nil {
		return notFound(err)
	}
	rt, err := s.rightsOf(ctx, s.q, cur)
	if err != nil {
		return err
	}
	if !rt.editable() {
		return errNotEditable()
	}
	if rt.draft {
		return nil
	}
	if f.RequirementCode == nil {
		if err := rt.canExtra(f.ItemID, f.Locked); err != nil {
			return err
		}
		return s.markAnswered(ctx, s.q, id, cur, "extra", f.ItemID.String())
	}
	key := docKey(*f.RequirementCode, f.AmendmentID)
	_, cl, err := s.viewWithChecklist(ctx, s.q, cur, Actor{})
	if err != nil {
		return err
	}
	if !rt.canDocument(key, missingIn(cl, key)) {
		return errNotReturned()
	}
	return s.markAnswered(ctx, s.q, id, cur, "document", key)
}

// RequestUpload registra o arquivo como pendente e autoriza o envio direto ao armazenamento.
func (s *Service) RequestUpload(ctx context.Context, actor Actor, id uuid.UUID, code string, in FileInput, upd authz.Decision) (DocFile, storage.Upload, error) {
	return s.requestUpload(ctx, actor, id, in, maxFilesPerItem, func(q *db.Queries) (db.ProjectDocumentItem, error) {
		item, _, err := s.lockedItem(ctx, q, id, code, in.AmendmentID, upd, actor)
		return item, err
	})
}

// RequestExtraUpload é o envio do arquivo de um documento adicional (um só por documento).
func (s *Service) RequestExtraUpload(ctx context.Context, actor Actor, id, itemID uuid.UUID, in FileInput, upd authz.Decision) (DocFile, storage.Upload, error) {
	return s.requestUpload(ctx, actor, id, in, 1, func(q *db.Queries) (db.ProjectDocumentItem, error) {
		item, _, err := s.lockedExtra(ctx, q, id, &itemID, upd)
		return item, err
	})
}

// requestUpload registra o arquivo como pendente no item que resolve devolve (já com o projeto travado) e autoriza o envio.
func (s *Service) requestUpload(ctx context.Context, actor Actor, id uuid.UUID, in FileInput, perItem int64, resolve func(q *db.Queries) (db.ProjectDocumentItem, error)) (DocFile, storage.Upload, error) {
	if err := s.requireStore(); err != nil {
		return DocFile{}, storage.Upload{}, err
	}
	s.settle(ctx, id)
	name, issues := upload.Check(in.Filename, in.ContentType, in.SizeBytes, upload.FieldNames{Filename: "filename", ContentType: "contentType", Size: "sizeBytes"})
	if len(issues) > 0 {
		return DocFile{}, storage.Upload{}, httpapi.ErrValidation(map[string]any{"fields": issues})
	}
	fileID := uuid.New()
	// A chave é gerada aqui: nada do que o cliente envia entra nela.
	key := fmt.Sprintf("projects/%s/%s", id, fileID)
	var row db.ProjectFile
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		item, err := resolve(q)
		if err != nil {
			return err
		}
		n, err := q.CountProjectItemFiles(ctx, item.ID)
		if err != nil {
			return err
		}
		if n >= perItem {
			return httpapi.ErrConflict("too_many_files", fmt.Sprintf("Este documento já tem %d arquivo(s) (limite).", perItem))
		}
		totals, err := q.ProjectFileTotals(ctx, id)
		if err != nil {
			return err
		}
		switch {
		case totals.Pending >= maxPendingPerProject:
			return httpapi.ErrConflict("too_many_pending", "Há muitos envios em andamento neste projeto. Aguarde terminarem ou remova os incompletos.")
		case totals.Total >= maxFilesPerProject:
			return httpapi.ErrConflict("project_files_limit", fmt.Sprintf("O projeto atingiu o limite de %d arquivos.", maxFilesPerProject))
		case totals.Bytes+in.SizeBytes > maxBytesPerProject:
			return httpapi.ErrConflict("project_storage_full", "O projeto atingiu o limite de armazenamento (2 GB). Remova arquivos que não são mais necessários.")
		}
		row, err = q.InsertProjectFile(ctx, db.InsertProjectFileParams{ID: fileID, ItemID: item.ID, ObjectKey: key, Filename: name,
			ContentType: in.ContentType, SizeBytes: in.SizeBytes, CreatedByID: &actor.ID})
		return err
	})
	if err != nil {
		return DocFile{}, storage.Upload{}, err
	}
	up, err := s.store.PresignUpload(ctx, key, in.ContentType, upload.Rules[in.ContentType].MaxSize, s.uploadTTL)
	if err != nil {
		_, _ = s.q.DeleteProjectFile(ctx, fileID)
		return DocFile{}, storage.Upload{}, err
	}
	return docFile(row), up, nil
}

func docFile(f db.ProjectFile) DocFile {
	return DocFile{ID: f.ID, Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.SizeBytes, Status: f.Status, SHA256: str(f.Sha256), CreatedAt: f.CreatedAt}
}

// projectFile carrega o arquivo do projeto e confere a decisão de permissão sobre o registro.
func (s *Service) projectFile(ctx context.Context, id, fileID uuid.UUID, dec authz.Decision, forbidden func() *httpapi.APIError) (db.GetProjectFileRow, error) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return db.GetProjectFileRow{}, notFound(err)
	}
	if !dec.AllowsRecord(refOf(r).Getter()) {
		return db.GetProjectFileRow{}, forbidden()
	}
	f, err := s.q.GetProjectFile(ctx, db.GetProjectFileParams{ID: fileID, ProjectID: id})
	if err != nil {
		return db.GetProjectFileRow{}, notFound(err)
	}
	return f, nil
}

func rejectUnprocessable(field, msg string) error { return fieldIssue(field, msg) }

// ConfirmUpload confere o objeto no armazenamento (existência, tipo, tamanho e assinatura do formato) e confirma o arquivo.
// Um objeto que não passa é apagado junto com o registro.
func (s *Service) ConfirmUpload(ctx context.Context, id, fileID uuid.UUID, upd authz.Decision) (DocFile, error) {
	if err := s.requireStore(); err != nil {
		return DocFile{}, err
	}
	s.settle(ctx, id)
	f, err := s.projectFile(ctx, id, fileID, upd, httpapi.ErrForbidden)
	if err != nil {
		return DocFile{}, err
	}
	if f.Status == FileConfirmed {
		return docFile(db.ProjectFile{ID: f.ID, Filename: f.Filename, ContentType: f.ContentType, SizeBytes: f.SizeBytes, Status: f.Status, CreatedAt: f.CreatedAt}), nil
	}
	info, err := s.store.Stat(ctx, f.ObjectKey)
	if errors.Is(err, storage.ErrNotFound) {
		return DocFile{}, httpapi.ErrConflict("file_not_uploaded", "O arquivo ainda não foi enviado ao armazenamento.")
	}
	if err != nil {
		return DocFile{}, err
	}
	rule := upload.Rules[f.ContentType]
	reject := func(msg string) (DocFile, error) {
		_ = s.store.Delete(ctx, f.ObjectKey)
		_, _ = s.q.DeleteProjectFile(ctx, f.ID)
		return DocFile{}, rejectUnprocessable("file", msg)
	}
	if info.Size < 1 || info.Size > rule.MaxSize {
		return reject("O arquivo enviado excede o tamanho permitido para o tipo.")
	}
	if !strings.EqualFold(strings.TrimSpace(strings.SplitN(info.ContentType, ";", 2)[0]), f.ContentType) {
		return reject("O tipo do arquivo enviado não corresponde ao informado.")
	}
	head, err := s.store.Head(ctx, f.ObjectKey, 16)
	if err != nil {
		return DocFile{}, err
	}
	if !rule.Magic(head) {
		return reject("O conteúdo do arquivo não corresponde ao formato informado.")
	}
	if err := s.fileRights(ctx, id, f); err != nil {
		_ = s.store.Delete(ctx, f.ObjectKey)
		_, _ = s.q.DeleteProjectFile(ctx, f.ID)
		return DocFile{}, err
	}
	sum, infected, err := s.verifyContent(ctx, f.ObjectKey, info.Size)
	switch {
	case infected != nil:
		_ = s.audit.Record(ctx, s.q, auditSystem("project.file-infected", id, map[string]any{"file": f.ID, "signature": infected.Signature}))
		return reject("O arquivo foi recusado pela verificação de segurança (conteúdo malicioso).")
	case errors.Is(err, errContentChanged):
		return reject("O arquivo mudou durante a verificação. Envie-o novamente.")
	case errors.Is(err, scan.ErrScan):
		if s.log != nil {
			s.log.ErrorContext(ctx, "verificação de vírus indisponível", "project", id, "file", f.ID, "error", err)
		}
		return DocFile{}, errScanUnavailable
	case err != nil:
		return DocFile{}, err
	}
	row, err := s.q.ConfirmProjectFile(ctx, db.ConfirmProjectFileParams{ID: f.ID, SizeBytes: info.Size, Sha256: &sum})
	if err != nil {
		return DocFile{}, notFound(err)
	}
	return docFile(row), nil
}

var (
	errContentChanged  = errors.New("o conteúdo do objeto mudou")
	errScanUnavailable = &httpapi.APIError{Status: 503, Code: "scan_unavailable", Message: "Não foi possível verificar o arquivo agora. Tente enviar o arquivo novamente em instantes."}
)

// SetScanner liga a verificação de vírus. Com ela, um arquivo só é confirmado depois de verificado; se a verificação não puder
// ser feita, o arquivo fica pendente (nunca é liberado sem verificar).
func (s *Service) SetScanner(sc scan.Scanner) { s.scanner = sc }

// countReader conta os bytes lidos, para conferir que o arquivo lido é do tamanho esperado.
type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// verifyContent lê o objeto uma única vez em sequência: calcula o SHA-256 e, havendo scanner, verifica o conteúdo. Devolve o hash
// (hexadecimal) e, se o scanner marcou o arquivo, o resultado. size é o tamanho que o armazenamento informou.
func (s *Service) verifyContent(ctx context.Context, key string, size int64) (string, *scan.Result, error) {
	rc, err := s.store.Get(ctx, key)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	cr := &countReader{r: io.TeeReader(rc, h)}
	if s.scanner != nil {
		res, err := s.scanner.Scan(ctx, cr)
		if err != nil {
			return "", nil, err
		}
		if res.Infected {
			return "", &res, nil
		}
	}
	// Garante que o arquivo inteiro passou pelo hash, qualquer que seja o scanner.
	if _, err := io.Copy(io.Discard, cr); err != nil {
		return "", nil, err
	}
	if cr.n != size {
		return "", nil, errContentChanged
	}
	return hex.EncodeToString(h.Sum(nil)), nil, nil
}

// DownloadLink é a URL temporária de download.
type DownloadLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Download devolve uma URL temporária para baixar o arquivo confirmado (404 se a leitura não cobre o projeto).
func (s *Service) Download(ctx context.Context, actor Actor, id, fileID uuid.UUID, read authz.Decision) (DownloadLink, error) {
	if err := s.requireStore(); err != nil {
		return DownloadLink{}, err
	}
	f, err := s.projectFile(ctx, id, fileID, read, httpapi.ErrNotFound)
	if err != nil {
		return DownloadLink{}, err
	}
	if f.Status != FileConfirmed {
		return DownloadLink{}, httpapi.ErrNotFound()
	}
	u, exp, err := s.store.PresignDownload(ctx, f.ObjectKey, f.Filename, f.ContentType, s.downloadTTL)
	if err != nil {
		return DownloadLink{}, err
	}
	// Documentos têm dados pessoais (RG, CPF, comprovantes): todo acesso fica registrado.
	if err := s.audit.Record(ctx, s.q, actor.entry("project.file-download", id, map[string]any{"file": fileID, "document": str(f.RequirementCode)})); err != nil {
		return DownloadLink{}, err
	}
	return DownloadLink{URL: u, ExpiresAt: exp}, nil
}

// DeleteFile remove um arquivo. Arquivo que ainda não fez parte de uma versão enviada é apagado (objeto e registro); o que já
// fez, só sai do checklist e continua guardado, para a versão enviada permanecer íntegra. O item sem arquivo e sem justificativa some.
func (s *Service) DeleteFile(ctx context.Context, id, fileID uuid.UUID, upd authz.Decision) error {
	if err := s.requireStore(); err != nil {
		return err
	}
	s.settle(ctx, id)
	f, err := s.projectFile(ctx, id, fileID, upd, httpapi.ErrForbidden)
	if err != nil {
		return err
	}
	if f.SupersededAt != nil {
		return httpapi.ErrNotFound()
	}
	if err := s.fileRights(ctx, id, f); err != nil {
		return err
	}
	// Remover a declaração assinada do checklist cancela a declaração, para poder emiti-la de novo.
	if _, err := s.q.CancelProjectDeclarationOfFile(ctx, &fileID); err != nil {
		return err
	}
	if f.Locked {
		_, err = s.q.SupersedeProjectFile(ctx, fileID)
		return err
	}
	// Objeto primeiro: se falhar, o registro continua e a remoção pode ser repetida.
	if err := s.store.Delete(ctx, f.ObjectKey); err != nil {
		return err
	}
	if _, err := s.q.DeleteProjectFile(ctx, fileID); err != nil {
		return err
	}
	s.deleteObjects(ctx, []string{previewKey(fileID)}) // o PDF gerado para exibição, se houver
	_, err = s.q.DeleteProjectDocumentItemIfEmpty(ctx, f.ItemID)
	return err
}

// SetJustification grava (ou, vazia, remove) a justificativa de não envio de um documento.
func (s *Service) SetJustification(ctx context.Context, actor Actor, id uuid.UUID, code, amendmentID, text string, upd authz.Decision) (*Checklist, error) {
	s.settle(ctx, id)
	text = multiline(text)
	if tooLong(text, maxJustification) {
		return nil, fieldIssue("text", "Use até 2.000 caracteres.")
	}
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		item, req, err := s.lockedItem(ctx, q, id, code, amendmentID, upd, actor)
		if err != nil {
			return err
		}
		if text != "" && !req.row.AcceptsJustification {
			return fieldIssue("text", "Este documento não aceita justificativa de não envio.")
		}
		if _, err := q.SetProjectDocumentJustification(ctx, db.SetProjectDocumentJustificationParams{ID: item.ID, Justification: text}); err != nil {
			return err
		}
		_, err = q.DeleteProjectDocumentItemIfEmpty(ctx, item.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	_, cl, err := s.viewWithChecklist(ctx, s.q, r, actor)
	return cl, err
}

// deleteObjects apaga objetos já desvinculados do banco. É melhor esforço: uma falha vira log.
func (s *Service) deleteObjects(ctx context.Context, keys []string) {
	if s.store == nil {
		return
	}
	for _, k := range keys {
		if err := s.store.Delete(ctx, k); err != nil && s.log != nil {
			s.log.ErrorContext(ctx, "removendo objeto órfão", "key", k, "error", err)
		}
	}
}

// CleanupPendingFiles remove envios que nunca foram confirmados há mais de 24 horas (o objeto, se existir, e o registro).
func (s *Service) CleanupPendingFiles(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, nil
	}
	rows, err := s.q.ListStalePendingProjectFiles(ctx, pendingGraceHours)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if err := s.store.Delete(ctx, r.ObjectKey); err != nil {
			return n, err
		}
		if _, err := s.q.DeleteProjectFile(ctx, r.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ---- Documentos adicionais ----

// ExtraInput é o título e a descrição de um documento adicional.
type ExtraInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func cleanExtra(in ExtraInput) (ExtraInput, error) {
	in.Title, in.Description = text(in.Title), multiline(in.Description)
	switch {
	case in.Title == "":
		return in, fieldIssue("title", "Informe o título do documento.")
	case tooLong(in.Title, maxExtraTitle):
		return in, fieldIssue("title", fmt.Sprintf("Use até %d caracteres.", maxExtraTitle))
	case tooLong(in.Description, maxExtraDescription):
		return in, fieldIssue("description", fmt.Sprintf("Use até %d caracteres.", maxExtraDescription))
	}
	return in, nil
}

// canExtra: no rascunho tudo vale; depois do envio, só durante uma complementação e só no que ainda não fez parte de uma versão
// enviada (o adicionado agora) ou no que a equipe devolveu (item é o id do documento adicional; uuid.Nil na criação). O que é novo não
// depende de a equipe ter devolvido algo: o documento é do proponente.
func (r rights) canExtra(item uuid.UUID, locked bool) error {
	if r.draft || (r.open != nil && (!locked || r.open.extras[item.String()])) {
		return nil
	}
	if r.open != nil {
		return errNotReturned()
	}
	return errNotEditable()
}

// lockedExtra trava o projeto, confere a permissão e o direito de mexer em documentos adicionais agora. Com itemID, devolve o
// documento (que precisa ser do projeto e ainda não enviado numa versão); sem ele, devolve só a checagem (criação).
func (s *Service) lockedExtra(ctx context.Context, q *db.Queries, id uuid.UUID, itemID *uuid.UUID, upd authz.Decision) (db.ProjectDocumentItem, db.Project, error) {
	cur, err := q.GetProjectForUpdate(ctx, id)
	if err != nil {
		return db.ProjectDocumentItem{}, cur, notFound(err)
	}
	if !upd.AllowsRecord(refOf(cur).Getter()) {
		return db.ProjectDocumentItem{}, cur, httpapi.ErrForbidden()
	}
	rt, err := s.rightsOf(ctx, q, cur)
	if err != nil {
		return db.ProjectDocumentItem{}, cur, err
	}
	if !rt.editable() {
		return db.ProjectDocumentItem{}, cur, errNotEditable()
	}
	if itemID == nil {
		return db.ProjectDocumentItem{}, cur, rt.canExtra(uuid.Nil, false)
	}
	item, err := q.GetExtraDocumentItem(ctx, db.GetExtraDocumentItemParams{ID: *itemID, ProjectID: id})
	if err != nil {
		return db.ProjectDocumentItem{}, cur, notFound(err)
	}
	locked, err := q.CountItemLockedFiles(ctx, item.ID)
	if err != nil {
		return db.ProjectDocumentItem{}, cur, err
	}
	if err := rt.canExtra(item.ID, locked > 0); err != nil {
		return db.ProjectDocumentItem{}, cur, err
	}
	return item, cur, s.markAnswered(ctx, q, id, cur, "extra", item.ID.String())
}

func (s *Service) checklistOf(ctx context.Context, id uuid.UUID, actor Actor) (*Checklist, error) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	_, cl, err := s.viewWithChecklist(ctx, s.q, r, actor)
	return cl, err
}

// CreateExtraDocument cria um documento adicional (ainda sem arquivo). Há no máximo maxExtraDocuments por projeto.
func (s *Service) CreateExtraDocument(ctx context.Context, actor Actor, id uuid.UUID, in ExtraInput, upd authz.Decision) (*Checklist, error) {
	s.settle(ctx, id)
	in, err := cleanExtra(in)
	if err != nil {
		return nil, err
	}
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, _, err := s.lockedExtra(ctx, q, id, nil, upd); err != nil {
			return err
		}
		n, err := q.CountExtraDocumentItems(ctx, id)
		if err != nil {
			return err
		}
		if n >= maxExtraDocuments {
			return httpapi.ErrConflict("too_many_extra_documents", fmt.Sprintf("O projeto já tem %d documentos adicionais (limite).", maxExtraDocuments))
		}
		_, err = q.CreateExtraDocumentItem(ctx, db.CreateExtraDocumentItemParams{ProjectID: id, Title: in.Title, Description: in.Description})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.checklistOf(ctx, id, actor)
}

// UpdateExtraDocument altera o título e a descrição de um documento adicional.
func (s *Service) UpdateExtraDocument(ctx context.Context, actor Actor, id, itemID uuid.UUID, in ExtraInput, upd authz.Decision) (*Checklist, error) {
	s.settle(ctx, id)
	in, err := cleanExtra(in)
	if err != nil {
		return nil, err
	}
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		item, _, err := s.lockedExtra(ctx, q, id, &itemID, upd)
		if err != nil {
			return err
		}
		_, err = q.UpdateExtraDocumentItem(ctx, db.UpdateExtraDocumentItemParams{ID: item.ID, Title: in.Title, Description: in.Description})
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.checklistOf(ctx, id, actor)
}

// DeleteExtraDocument remove o documento adicional e o arquivo dele. Não remove o que já fez parte de uma versão enviada.
func (s *Service) DeleteExtraDocument(ctx context.Context, actor Actor, id, itemID uuid.UUID, upd authz.Decision) (*Checklist, error) {
	if err := s.requireStore(); err != nil {
		return nil, err
	}
	s.settle(ctx, id)
	var keys []string
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		item, _, err := s.lockedExtra(ctx, q, id, &itemID, upd)
		if err != nil {
			return err
		}
		// O devolvido que já foi numa versão enviada não se exclui (a versão perderia o arquivo): troca-se o arquivo.
		locked, err := q.CountItemLockedFiles(ctx, item.ID)
		if err != nil {
			return err
		}
		if locked > 0 {
			return httpapi.ErrConflict("extra_document_sent", "Este documento já foi enviado e não pode ser excluído. Troque o arquivo.")
		}
		if keys, err = q.ListItemFileKeys(ctx, item.ID); err != nil {
			return err
		}
		_, err = q.DeleteExtraDocumentItem(ctx, item.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.deleteObjects(ctx, keys)
	return s.checklistOf(ctx, id, actor)
}
