package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/pdfbuild"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
	"github.com/wagner/convenios-e-fomentos/backend/internal/sigadoc"
)

// Abertura do processo do projeto no SIGADOC (docs/planejamento-integracao-sigadoc.md, seções 6.2 e 6.3).
//
// O analista responsável confere o Termo, informa o PIN UMA vez e o sistema monta o processo em segundo plano: cria, finaliza, assina e
// autua o Termo (o que dá o número do processo), depois cria o relatório do projeto e cada arquivo do proponente como documento
// capturado, assina e junta ao processo. O PIN vive só na memória da tarefa (sigadoc.PIN): nunca vai para banco, auditoria, log nem
// mensagem de erro. Se a API cair, a tarefa fica interrompida e o analista a retoma com o PIN de novo: só o que falta é refeito.

const (
	taskRunning     = "running"
	taskInterrupted = "interrupted"
	taskCompleted   = "completed"
	taskCancelled   = "cancelled"

	itemTermo    = "termo"
	itemReport   = "report"
	itemDocument = "document"

	// Estados de um item: veja a migração 00042.
	itemPending   = "pending"
	itemCreating  = "creating"
	itemCreated   = "created"
	itemFinalized = "finalized"
	itemSigned    = "signed"
	itemJoined    = "joined"
	itemFailed    = "failed"

	actionSigadocOpen   = "project.sigadoc-open"
	actionSigadocResume = "project.sigadoc-resume"
	actionSigadocCancel = "project.sigadoc-cancel"
	actionSigadocReady  = "project.sigadoc-ready"

	// Quantas vezes por janela um analista pode informar o PIN (cada tentativa errada conta para o bloqueio do PIN no SIGADOC).
	pinTriesMax    = 5
	pinTriesWindow = 10 * time.Minute

	maxTermoFields = 50
	maxFieldLen    = 2000
)

var errSigadocUnavailable = &httpapi.APIError{Status: 503, Code: "sigadoc_unavailable", Message: "O SIGADOC não está disponível agora."}

// errCancelled: o projeto deixou de estar na abertura (foi arquivado, negado ou cancelado): a montagem para.
var errCancelled = errors.New("montagem cancelada")

// itemFailure é um item que não pôde ser preparado (arquivo corrompido, página grande demais...): não impede os outros, mas a tarefa
// termina interrompida para o analista ver e retomar.
type itemFailure struct{ msg string }

func (e *itemFailure) Error() string { return e.msg }

// ---- Ligação ao serviço ----

// SetSigadocLookup define a matrícula com que o usuário de sistema se autentica só para procurar a matrícula de cada analista.
func (s *Service) SetSigadocLookup(matricula string) {
	s.sigadocLookup = strings.ToUpper(strings.TrimSpace(matricula))
}

// SetSigadocContext define o contexto das montagens em segundo plano (o da aplicação: termina no desligamento).
func (s *Service) SetSigadocContext(ctx context.Context) { s.sigadocBG = ctx }

// InterruptStaleSigadocTasks marca como interrompidas as montagens que estavam em execução quando a API caiu: o PIN se perdeu.
func (s *Service) InterruptStaleSigadocTasks(ctx context.Context) (int64, error) {
	return s.q.InterruptRunningSigadocTasks(ctx)
}

// WaitSigadoc espera as montagens em andamento terminarem, por no máximo timeout. Devolve false se ainda havia alguma.
func (s *Service) WaitSigadoc(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { s.sigadocWG.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

type sigadocRuns struct {
	mu     sync.Mutex
	cancel map[uuid.UUID]context.CancelFunc
	tries  map[uuid.UUID][]time.Time
}

func (r *sigadocRuns) init() {
	if r.cancel == nil {
		r.cancel = map[uuid.UUID]context.CancelFunc{}
		r.tries = map[uuid.UUID][]time.Time{}
	}
}

// allow registra uma tentativa de PIN do administrador e diz se ainda cabe na janela.
func (s *Service) pinAllowed(admin uuid.UUID) bool {
	s.runs.mu.Lock()
	defer s.runs.mu.Unlock()
	s.runs.init()
	now := s.now()
	kept := s.runs.tries[admin][:0:0]
	for _, t := range s.runs.tries[admin] {
		if now.Sub(t) < pinTriesWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= pinTriesMax {
		s.runs.tries[admin] = kept
		return false
	}
	s.runs.tries[admin] = append(kept, now)
	return true
}

// ---- Dados do Termo ----

// Campos da entrevista do modelo "Termo de Abertura de Expediente/Processo no SIGADOC" (lidos em GET /sigaex/modelos/{id}).
const (
	termoSolicitante = "auxGovMT_interessadoProcurador"
	termoNome        = "docGovMT_interessado_nome"
	termoDocumento   = "docGovMT_cpf-cnpj"
	termoTelefone    = "docGovMT_telefone"
	termoEmail       = "docGovMT_email"
	termoAnexoFisico = "docGovMT_anexoFisico"
	termoResumo      = "docGovMT_resumoAssunto"
	termoDeveJuntar  = "auxGovMT_deveJuntar"
)

// termoRequired são os campos do Termo que o SIGADOC exige preenchidos para criar o documento.
var termoRequired = []string{termoNome, termoDocumento, termoTelefone, termoEmail, termoResumo}

// TermoFields são os campos do Termo de Abertura montados com os dados do projeto, com os nomes da entrevista do modelo no SIGADOC
// (o parâmetro sigadoc_settings.field_map ainda pode renomear cada um, se o modelo mudar). O interessado é a instituição; o contato
// vem da instituição e, na falta dele, do proponente. O "Atendimento" fica em branco (não é obrigatório e o modelo só oferece presencial ou e-mail, mas o projeto chega pelo sistema).
// O analista pode ajustar os valores antes de assinar.
func TermoFields(p *Project) map[string]string {
	phone, email := p.Institution.Phone, p.Institution.Email
	if phone == "" && len(p.Proponent.Phones) > 0 {
		phone = p.Proponent.Phones[0].Number
	}
	if email == "" && len(p.Proponent.Emails) > 0 {
		email = p.Proponent.Emails[0].Address
	}
	summary := "Solicitação de apoio financeiro: " + p.Proposal.Title
	if ins := labelOr(instrumentText, p.Institution.Instrument); ins != "" {
		summary = "Solicitação de " + ins + " " + p.Code + ": " + p.Proposal.Title
	}
	return map[string]string{
		termoSolicitante: "interessado",
		termoNome:        p.Institution.Name,
		termoDocumento:   formatCNPJ(p.Institution.CNPJ),
		termoTelefone:    phoneDigitsText(phone),
		termoEmail:       email,
		termoAnexoFisico: "Nao",
		termoResumo:      summary,
		termoDeveJuntar:  "não",
	}
}

// phoneDigitsText formata um telefone só com dígitos e DDD; outro formato segue como veio.
func phoneDigitsText(n string) string {
	switch len(n) {
	case 11:
		return fmt.Sprintf("(%s) %s-%s", n[:2], n[2:7], n[7:])
	case 10:
		return fmt.Sprintf("(%s) %s-%s", n[:2], n[2:6], n[6:])
	}
	return n
}

// termoMissing devolve o primeiro campo obrigatório vazio (nome, CPF/CNPJ, telefone, e-mail e resumo), para pedir o ajuste antes de
// abrir o processo. A pré-visualização não exige: é por ela que o analista preenche.
func termoMissing(fields map[string]string) string {
	for _, k := range termoRequired {
		if strings.TrimSpace(fields[k]) == "" {
			return k
		}
	}
	return ""
}

// termoInterview aplica os ajustes do analista e o mapa de nomes do parâmetro, e devolve os campos como o modelo os espera.
func termoInterview(base map[string]string, overrides map[string]string, fieldMap map[string]string) (map[string]string, error) {
	if len(overrides) > maxTermoFields {
		return nil, fieldIssue("fields", "Muitos campos.")
	}
	merged := map[string]string{}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overrides {
		k = strings.TrimSpace(k)
		if k == "" || len([]rune(k)) > 100 || len([]rune(v)) > maxFieldLen || strings.ContainsRune(v, 0) {
			return nil, fieldIssue("fields", "Campo do Termo inválido.")
		}
		merged[k] = strings.TrimSpace(v)
	}
	out := make(map[string]string, len(merged))
	for k, v := range merged {
		if mapped := strings.TrimSpace(fieldMap[k]); mapped != "" {
			k = mapped
		}
		out[k] = v
	}
	return out, nil
}

// ---- Estado ----

// SigadocItem é um item da montagem, como a tela o mostra.
type SigadocItem struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Part   int    `json:"part"`
	Parts  int    `json:"parts"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// SigadocTask é uma tentativa de montagem do processo.
type SigadocTask struct {
	ID            uuid.UUID     `json:"id"`
	Status        string        `json:"status"`
	Attempt       int           `json:"attempt"`
	ProgressDone  int           `json:"progressDone"`
	ProgressTotal int           `json:"progressTotal"`
	ErrorCode     string        `json:"errorCode"`
	Error         string        `json:"error"`
	StartedAt     time.Time     `json:"startedAt"`
	FinishedAt    *time.Time    `json:"finishedAt"`
	Items         []SigadocItem `json:"items"`
}

// SigadocOpening diz se o botão "Abrir no SIGADOC" cabe agora e, se não, por quê.
type SigadocOpening struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	// Configured: o SIGADOC está ligado no servidor. TermoReady: o parâmetro sigadoc_settings tem o id do modelo do Termo.
	Configured bool `json:"configured"`
	TermoReady bool `json:"termoReady"`
}

func (s *Service) sigadocTaskView(ctx context.Context, q *db.Queries, projectID uuid.UUID) (*SigadocTask, error) {
	t, err := q.GetLatestSigadocTask(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // sem tarefa ainda
	}
	if err != nil {
		return nil, err
	}
	items, err := q.ListSigadocItems(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := &SigadocTask{ID: t.ID, Status: t.Status, Attempt: int(t.Attempt), ProgressDone: int(t.ProgressDone), ProgressTotal: int(t.ProgressTotal),
		ErrorCode: t.ErrorCode, Error: t.Error, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt, Items: []SigadocItem{}}
	done := 0
	for _, it := range items {
		if it.Status == itemJoined {
			done++
		}
		out.Items = append(out.Items, SigadocItem{Kind: it.Kind, Label: it.Label, Part: int(it.Part), Parts: int(it.Parts), Status: it.Status, Error: it.Error})
	}
	if t.Status == taskRunning {
		// A tela acompanha o que está gravado agora, não o que o último "heartbeat" guardou.
		out.ProgressDone, out.ProgressTotal = done, len(items)
	}
	return out, nil
}

func (s *Service) sigadocOpening(ctx context.Context, q *db.Queries, r db.Project, process bool, task *SigadocTask) SigadocOpening {
	o := SigadocOpening{Configured: s.sigadoc != nil}
	if snap, err := s.rules.SnapshotAt(ctx, q, s.today()); err == nil && snap.Sigadoc != nil {
		o.TermoReady = snap.Sigadoc.TermoModelID != ""
	}
	switch {
	case s.sigadoc == nil:
		o.Reason = "O SIGADOC não está ligado neste ambiente."
	case r.Status != StatusSubmitted:
		o.Reason = "O projeto não está ativo."
	case str(r.Stage) != StageOpeningSigadoc:
		o.Reason = "O processo só se abre na etapa \"Abertura do processo no SIGADOC\"."
	case process:
		o.Reason = "O processo já foi aberto no SIGADOC."
	case task != nil && task.Status == taskRunning:
		o.Reason = "A montagem do processo está em andamento."
	case task != nil && task.Status == taskInterrupted:
		o.Reason = "A montagem foi interrompida: use \"Retomar\" com o PIN."
	case !o.TermoReady:
		o.Reason = "Falta configurar o id do modelo do Termo (parâmetro sigadoc_settings)."
	default:
		o.Available = true
	}
	return o
}

// ---- Pré-visualização do Termo ----

// TermoInput são os ajustes do analista nos campos do Termo.
type TermoInput struct {
	Fields map[string]string `json:"fields"`
}

// TermoPreview é o Termo preenchido, para o analista conferir antes de assinar. Nada foi gravado no SIGADOC.
type TermoPreview struct {
	HTML string `json:"html"`
	// Fields são os campos lógicos com os valores usados (o analista os ajusta e pede de novo).
	Fields map[string]string `json:"fields"`
}

// gatewayText é o que o analista lê quando o X-Via recusa: o detalhe técnico fica fora da tela.
// prohibitedHint acompanha a recusa por configuração do SIGADOC (que já diz qual documento e qual usuário).
const prohibitedHint = " Peça à MTI a liberação dessa operação para o seu usuário e a sua lotação no SIGADOC."

const gatewayText = "O X-Via (o servidor de segurança da MTI que liga ao SIGADOC) recusou o pedido, e isso não depende do seu PIN nem deste cadastro. Avise a MTI e tente de novo depois."

func mapSigadocError(err error) error {
	var se *sigadoc.Error
	switch {
	case err == nil:
		return nil
	case sigadoc.IsPINRejected(err):
		return validationCode("pin_rejected", "O SIGADOC recusou o PIN. Confira-o antes de tentar de novo: tentativas erradas podem bloquear o seu PIN.")
	case sigadoc.IsForbidden(err):
		return &httpapi.APIError{Status: 502, Code: "sigadoc_forbidden", Message: "O usuário de sistema não tem acesso a este método do SIGADOC. Peça a liberação à MTI."}
	case sigadoc.IsUnknownIdentity(err):
		return validationCode("sigadoc_no_matricula", "O SIGADOC não conhece a matrícula deste analista.")
	case sigadoc.IsProhibited(err):
		// IsProhibited já garante que é um *sigadoc.Error; `se` só é preenchida no último case, por isso o As próprio.
		var pe *sigadoc.Error
		_ = errors.As(err, &pe)
		return &httpapi.APIError{Status: 502, Code: "sigadoc_prohibited", Message: truncate(pe.Message, 300) + prohibitedHint}
	case sigadoc.IsGatewayError(err):
		return &httpapi.APIError{Status: 502, Code: "sigadoc_gateway", Message: gatewayText}
	case errors.Is(err, sigadoc.ErrUnsupported):
		return &httpapi.APIError{Status: 501, Code: "sigadoc_unsupported", Message: "Esta operação ainda não é suportada."}
	case errors.As(err, &se):
		return &httpapi.APIError{Status: 502, Code: "sigadoc_error", Message: "O SIGADOC recusou o pedido: " + truncate(se.Message, 200)}
	}
	return &httpapi.APIError{Status: 502, Code: "sigadoc_error", Message: "Não foi possível falar com o SIGADOC agora."}
}

// openGate confere o que vale para pré-visualizar e para abrir: SIGADOC ligado, projeto na abertura, analista responsável e sem processo.
func (s *Service) openGate(ctx context.Context, q *db.Queries, actor Actor, id uuid.UUID, read authz.Decision, resume bool) (db.Project, *rules.SigadocSettings, error) {
	if s.sigadoc == nil {
		return db.Project{}, nil, errSigadocUnavailable
	}
	cur, err := s.lockedForReviewNoTx(ctx, q, id, read)
	if err != nil {
		return db.Project{}, nil, err
	}
	if str(cur.Stage) != StageOpeningSigadoc {
		return db.Project{}, nil, errStage(str(cur.Stage))
	}
	if cur.AnalystID == nil || *cur.AnalystID != actor.ID {
		if err := s.ensureResponsible(ctx, q, cur, actor); err != nil {
			return db.Project{}, nil, err
		}
		return db.Project{}, nil, httpapi.ErrConflict("not_responsible_analyst", "Só o analista responsável abre o processo no SIGADOC. Assuma o projeto.")
	}
	// O processo é registrado assim que o Termo é autuado, antes do relatório e dos arquivos. Por isso a retomada de uma montagem
	// interrompida (que o chamador confere em seguida) precisa passar com o processo já registrado: só o que falta é refeito.
	if _, err := q.GetProjectSigadoc(ctx, id); err == nil && !resume {
		return db.Project{}, nil, httpapi.ErrConflict("sigadoc_process_exists", "O processo deste projeto já foi aberto no SIGADOC.")
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return db.Project{}, nil, err
	}
	snap, err := s.rules.SnapshotAt(ctx, q, s.today())
	if err != nil {
		return db.Project{}, nil, err
	}
	if snap.Sigadoc == nil || snap.Sigadoc.TermoModelID == "" {
		return db.Project{}, nil, validationCode("sigadoc_not_configured", "Falta configurar o id do modelo do Termo de Abertura (parâmetro sigadoc_settings).")
	}
	return cur, snap.Sigadoc, nil
}

// lockedForReviewNoTx é lockedForReview sem travar a linha: a pré-visualização só lê.
func (s *Service) lockedForReviewNoTx(ctx context.Context, q *db.Queries, id uuid.UUID, read authz.Decision) (db.Project, error) {
	cur, err := q.GetProject(ctx, id)
	if err != nil {
		return db.Project{}, notFound(err)
	}
	if !read.AllowsRecord(refOf(cur).Getter()) {
		return db.Project{}, httpapi.ErrForbidden()
	}
	if cur.Status != StatusSubmitted {
		return db.Project{}, httpapi.ErrConflict("project_not_submitted", "Só projetos enviados e ativos abrem processo no SIGADOC.")
	}
	return cur, nil
}

// resolveMatricula descobre a matrícula do analista pelo CPF e devolve o token que a representa.
func (s *Service) resolveMatricula(ctx context.Context, actor Actor) (string, sigadoc.Token, error) {
	if s.sigadocLookup == "" {
		return "", sigadoc.Token{}, validationCode("sigadoc_not_configured", "Falta configurar SIGADOC_LOOKUP_MATRICULA no servidor.")
	}
	if actor.CPF == "" {
		return "", sigadoc.Token{}, validationCode("sigadoc_no_matricula", "Seu cadastro não tem CPF: não dá para achar a sua matrícula no SIGADOC.")
	}
	look, err := s.sigadoc.Token(ctx, s.sigadocLookup)
	if err != nil {
		return "", sigadoc.Token{}, mapSigadocError(err)
	}
	list, err := s.sigadoc.Client().MatriculaByCPF(ctx, look, actor.CPF, "SEDEC")
	if err != nil {
		return "", sigadoc.Token{}, mapSigadocError(err)
	}
	if len(list) == 0 {
		return "", sigadoc.Token{}, validationCode("sigadoc_no_matricula", "O SIGADOC não tem matrícula da SEDEC para o seu CPF.")
	}
	sort.Strings(list)
	tok, err := s.sigadoc.Token(ctx, list[0])
	if err != nil {
		return "", sigadoc.Token{}, mapSigadocError(err)
	}
	return list[0], tok, nil
}

// PreviewTermo gera o Termo preenchido com os dados do projeto (e os ajustes do analista) para conferência. Nada é gravado.
func (s *Service) PreviewTermo(ctx context.Context, actor Actor, id uuid.UUID, in TermoInput, read authz.Decision) (*TermoPreview, error) {
	cur, set, err := s.openGate(ctx, s.q, actor, id, read, false)
	if err != nil {
		return nil, err
	}
	v, err := s.view(ctx, s.q, cur, actor)
	if err != nil {
		return nil, err
	}
	base := TermoFields(v)
	interview, err := termoInterview(base, in.Fields, set.FieldMap)
	if err != nil {
		return nil, err
	}
	_, tok, err := s.resolveMatricula(ctx, actor)
	if err != nil {
		return nil, err
	}
	html, err := s.sigadoc.Client().PreviewTermo(ctx, tok, set.TermoModelID, interview)
	if err != nil {
		return nil, mapSigadocError(err)
	}
	shown := map[string]string{}
	for k, val := range base {
		shown[k] = val
	}
	for k, val := range in.Fields {
		shown[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	return &TermoPreview{HTML: html, Fields: shown}, nil
}

// ---- Abrir, retomar e cancelar ----

// OpenInput é o pedido de abertura: o Termo conferido e o PIN. O PIN nunca é registrado.
type OpenInput struct {
	Fields map[string]string `json:"fields"`
	PIN    string            `json:"pin"`
}

// OpenSigadoc valida o PIN e inicia a montagem do processo em segundo plano. Responde na hora, com a tarefa em execução.
func (s *Service) OpenSigadoc(ctx context.Context, actor Actor, id uuid.UUID, in OpenInput, read authz.Decision) (*SigadocView, error) {
	return s.startSigadoc(ctx, actor, id, in, read, false)
}

// ResumeSigadoc retoma uma montagem interrompida com o PIN de novo: só o que falta é refeito.
func (s *Service) ResumeSigadoc(ctx context.Context, actor Actor, id uuid.UUID, in OpenInput, read authz.Decision) (*SigadocView, error) {
	return s.startSigadoc(ctx, actor, id, in, read, true)
}

func (s *Service) startSigadoc(ctx context.Context, actor Actor, id uuid.UUID, in OpenInput, read authz.Decision, resume bool) (*SigadocView, error) {
	pin := sigadoc.NewPIN(strings.TrimSpace(in.PIN))
	handedOver := false
	defer func() {
		if !handedOver {
			pin.Zero()
		}
	}()
	in.PIN = ""
	if n := len(pin); n < 4 || n > 32 {
		return nil, validationCode("pin_invalid", "Informe o PIN de assinatura do SIGADOC.")
	}
	if !s.pinAllowed(actor.ID) {
		return nil, httpapi.ErrTooManyRequests()
	}
	cur, set, err := s.openGate(ctx, s.q, actor, id, read, resume)
	if err != nil {
		return nil, err
	}
	last, lerr := s.q.GetLatestSigadocTask(ctx, id)
	switch {
	case lerr != nil && !errors.Is(lerr, pgx.ErrNoRows):
		return nil, lerr
	case lerr == nil && last.Status == taskRunning:
		return nil, httpapi.ErrConflict("sigadoc_task_running", "A montagem do processo já está em andamento.")
	case resume && (errors.Is(lerr, pgx.ErrNoRows) || last.Status != taskInterrupted):
		return nil, httpapi.ErrConflict("sigadoc_nothing_to_resume", "Não há montagem interrompida para retomar.")
	case !resume && lerr == nil && last.Status == taskInterrupted:
		return nil, httpapi.ErrConflict("sigadoc_task_interrupted", "A montagem foi interrompida: use \"Retomar\" com o PIN.")
	}

	matricula, tok, err := s.resolveMatricula(ctx, actor)
	if err != nil {
		return nil, err
	}
	// Valida o PIN ANTES de criar a tarefa: não gasta tentativas dentro da montagem.
	if err := s.sigadoc.Client().ValidatePIN(ctx, tok, actor.CPF, pin); err != nil {
		return nil, mapSigadocError(err)
	}

	v, err := s.view(ctx, s.q, cur, actor)
	if err != nil {
		return nil, err
	}
	base := TermoFields(v)
	merged := map[string]string{}
	for k, val := range base {
		merged[k] = val
	}
	for k, val := range in.Fields {
		merged[strings.TrimSpace(k)] = strings.TrimSpace(val)
	}
	if k := termoMissing(merged); k != "" {
		return nil, fieldIssue("fields."+k, "Preencha este campo do Termo antes de abrir o processo.")
	}
	interview, err := termoInterview(base, in.Fields, set.FieldMap)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(interview)
	if err != nil {
		return nil, err
	}

	var task db.SigadocTask
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		locked, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if locked.Status != StatusSubmitted || str(locked.Stage) != StageOpeningSigadoc || locked.AnalystID == nil || *locked.AnalystID != actor.ID {
			return httpapi.ErrConflict("project_stage_invalid", "O projeto mudou: recarregue a página.")
		}
		task, err = q.CreateSigadocTask(ctx, db.CreateSigadocTaskParams{ProjectID: id, StartedByID: &actor.ID, Matricula: matricula})
		if err != nil {
			if isUniqueViolation(err) {
				return httpapi.ErrConflict("sigadoc_task_running", "A montagem do processo já está em andamento.")
			}
			return err
		}
		desc := "Termo de Abertura: projeto " + str(locked.Code)
		if err := q.UpsertSigadocItem(ctx, db.UpsertSigadocItemParams{ProjectID: id, Key: "termo", Seq: 0, Kind: itemTermo, Label: "Termo de Abertura",
			Description: desc, Part: 1, Parts: 1, Payload: payload}); err != nil {
			return err
		}
		if err := q.UpsertSigadocItem(ctx, db.UpsertSigadocItemParams{ProjectID: id, Key: "report", Seq: 100, Kind: itemReport, Label: "Relatório do projeto",
			Description: "Relatório do projeto " + str(locked.Code) + ": " + locked.InstitutionName, Part: 1, Parts: 1, Payload: []byte("{}")}); err != nil {
			return err
		}
		action := actionSigadocOpen
		if resume {
			action = actionSigadocResume
		}
		return s.audit.Record(ctx, q, actor.entry(action, id, map[string]any{"task": task.ID, "attempt": task.Attempt}))
	})
	if err != nil {
		return nil, err
	}

	run := &openRun{svc: s, task: task, projectID: id, actor: actor, read: read, pin: pin, settings: *set}
	s.runs.mu.Lock()
	s.runs.init()
	bg := s.sigadocBG
	if bg == nil {
		bg = context.Background()
	}
	rctx, cancel := context.WithCancel(bg)
	s.runs.cancel[id] = cancel
	s.runs.mu.Unlock()
	handedOver = true
	s.sigadocWG.Add(1)
	go run.execute(rctx)

	return s.sigadocView(ctx, s.q, cur)
}

// CancelSigadoc cancela a montagem em andamento (só o analista responsável). O que já foi inserido permanece no SIGADOC.
func (s *Service) CancelSigadoc(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) (*SigadocView, error) {
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) {
		return nil, httpapi.ErrForbidden()
	}
	if r.AnalystID == nil || *r.AnalystID != actor.ID {
		return nil, httpapi.ErrConflict("not_responsible_analyst", "Só o analista responsável cancela a montagem.")
	}
	last, err := s.q.GetLatestSigadocTask(ctx, id)
	if err != nil || last.Status != taskRunning {
		return nil, httpapi.ErrConflict("sigadoc_nothing_to_cancel", "Não há montagem em andamento.")
	}
	if err := s.audit.Record(ctx, s.q, actor.entry(actionSigadocCancel, id, map[string]any{"task": last.ID})); err != nil {
		return nil, err
	}
	s.runs.mu.Lock()
	cancel := s.runs.cancel[id]
	s.runs.mu.Unlock()
	if cancel != nil {
		cancel()
	} else {
		// Sem execução viva neste processo (por exemplo, outra instância da API a tinha): fecha a tarefa aqui.
		_ = s.q.FinishSigadocTask(ctx, db.FinishSigadocTaskParams{ID: last.ID, Status: taskCancelled, ErrorCode: "cancelled",
			Error: "Montagem cancelada. O que já foi inserido permanece no SIGADOC.", ProgressDone: last.ProgressDone, ProgressTotal: last.ProgressTotal})
	}
	return s.sigadocView(ctx, s.q, r)
}

// ---- A montagem ----

type openRun struct {
	svc         *Service
	task        db.SigadocTask
	projectID   uuid.UUID
	actor       Actor
	read        authz.Decision
	pin         sigadoc.PIN
	settings    rules.SigadocSettings
	tok         sigadoc.Token
	tmp         string
	institution string
	// failures são os arquivos que não puderam ser preparados (não impedem os outros).
	failures []string
}

// execute roda a montagem. É a única dona do PIN: zera-o ao terminar, de qualquer jeito.
func (r *openRun) execute(ctx context.Context) {
	s := r.svc
	defer s.sigadocWG.Done()
	defer r.pin.Zero()
	defer func() {
		s.runs.mu.Lock()
		if c := s.runs.cancel[r.projectID]; c != nil {
			c()
		}
		delete(s.runs.cancel, r.projectID)
		s.runs.mu.Unlock()
	}()
	// Contexto próprio para gravar o resultado mesmo se o de execução já terminou (desligamento ou cancelamento).
	fin := context.WithoutCancel(ctx)
	defer func() {
		if p := recover(); p != nil {
			s.logger().ErrorContext(fin, "pânico na montagem do processo no SIGADOC", "projectId", r.projectID, "task", r.task.ID)
			r.finish(fin, taskInterrupted, "internal", "Erro interno na montagem. Informe o PIN de novo para retomar.")
		}
	}()
	if dir, err := os.MkdirTemp(s.reportTmpRoot(), "sigadoc-*"); err == nil {
		r.tmp = dir
		defer func() { _ = os.RemoveAll(dir) }()
	}
	err := r.run(ctx, fin)
	switch {
	case err == nil:
		return
	case errors.Is(err, errCancelled):
		r.finish(fin, taskCancelled, "project_archived", "A montagem foi cancelada: o projeto não está mais na abertura do processo.")
	case ctx.Err() != nil && errors.Is(err, context.Canceled):
		// Cancelado pelo analista (a rota de cancelamento) ou pelo desligamento.
		if s.sigadocBG != nil && s.sigadocBG.Err() != nil {
			r.finish(fin, taskInterrupted, "shutdown", "A API foi desligada durante a montagem. Informe o PIN de novo para retomar.")
		} else {
			r.finish(fin, taskCancelled, "cancelled", "Montagem cancelada. O que já foi inserido permanece no SIGADOC.")
		}
	case sigadoc.IsPINRejected(err):
		r.finish(fin, taskInterrupted, "pin_rejected", "O SIGADOC recusou o PIN. Nada mais foi tentado, para não bloquear o seu PIN. Confira-o e retome.")
	case sigadoc.IsForbidden(err):
		r.finish(fin, taskInterrupted, "sigadoc_forbidden", "O usuário de sistema não tem acesso a um método do SIGADOC. Peça a liberação à MTI e retome.")
	default:
		var itf *itemFailure
		if errors.As(err, &itf) {
			r.finish(fin, taskInterrupted, "item_failed", itf.msg)
		} else {
			s.logger().WarnContext(fin, "montagem do processo no SIGADOC interrompida", "projectId", r.projectID, "task", r.task.ID, "error", watchErrorText(err))
			r.finish(fin, taskInterrupted, "sigadoc_error", "A montagem parou: "+watchErrorText(err)+" Informe o PIN de novo para retomar de onde parou.")
		}
	}
}

// finish grava o resultado da tarefa. O texto é curto e nunca leva PIN, URL, token nem corpo de resposta.
func (r *openRun) finish(ctx context.Context, status, code, msg string) {
	items, _ := r.svc.q.ListSigadocItems(ctx, r.projectID)
	done := 0
	for _, it := range items {
		if it.Status == itemJoined {
			done++
		}
	}
	if err := r.svc.q.FinishSigadocTask(ctx, db.FinishSigadocTaskParams{ID: r.task.ID, Status: status, ErrorCode: code, Error: truncate(msg, 400),
		ProgressDone: int32(done), ProgressTotal: int32(len(items))}); err != nil { //nolint:gosec // poucos itens
		r.svc.logger().ErrorContext(ctx, "gravando o resultado da montagem", "task", r.task.ID, "error", err)
	}
}

// active confere que o projeto continua na abertura do processo; senão a montagem para.
func (r *openRun) active(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := r.svc.q.GetProject(ctx, r.projectID)
	if err != nil {
		return err
	}
	if p.Status != StatusSubmitted || str(p.Stage) != StageOpeningSigadoc {
		return errCancelled
	}
	return nil
}

func (r *openRun) touch(ctx context.Context) {
	items, err := r.svc.q.ListSigadocItems(ctx, r.projectID)
	if err != nil {
		return
	}
	done := 0
	for _, it := range items {
		if it.Status == itemJoined {
			done++
		}
	}
	_ = r.svc.q.TouchSigadocTask(ctx, db.TouchSigadocTaskParams{ID: r.task.ID, ProgressDone: int32(done), ProgressTotal: int32(len(items))}) //nolint:gosec // poucos itens
}

func (r *openRun) run(ctx, fin context.Context) error {
	s := r.svc
	tok, err := s.sigadoc.Token(ctx, r.task.Matricula)
	if err != nil {
		return err
	}
	r.tok = tok
	items, err := s.q.ListSigadocItems(ctx, r.projectID)
	if err != nil {
		return err
	}
	byKey := map[string]db.SigadocItem{}
	for _, it := range items {
		byKey[it.Key] = it
	}

	// 1. Termo: criar, finalizar, assinar e autuar. O número do processo nasce aqui.
	if err := r.active(ctx); err != nil {
		return err
	}
	if err := r.advance(ctx, byKey["termo"], func() ([]byte, string, error) { return nil, "", nil }); err != nil {
		return err
	}

	// 2. Relatório do projeto, como documento interno capturado.
	if err := r.active(ctx); err != nil {
		return err
	}
	if err := r.advance(ctx, byKey["report"], r.reportContent); err != nil {
		return err
	}

	// 3. Documentos do proponente, na ordem do checklist e depois os adicionais, um arquivo (ou parte) por documento.
	data, err := r.reportData(ctx)
	if err != nil {
		return err
	}
	seq := 200
	for _, doc := range data.Documents {
		for fi, fl := range doc.Files {
			seq += 10
			if err := r.processFile(ctx, data, doc, fl, fi+1, len(doc.Files), seq); err != nil {
				var itf *itemFailure
				if errors.As(err, &itf) {
					r.failures = append(r.failures, itf.msg)
					continue
				}
				return err
			}
		}
	}
	if len(r.failures) > 0 {
		return &itemFailure{msg: fmt.Sprintf("%d arquivo(s) não puderam ser enviados ao SIGADOC: %s Corrija e retome.", len(r.failures), truncate(strings.Join(r.failures, " "), 250))}
	}
	return r.complete(ctx, fin)
}

// complete confere que tudo foi juntado e leva o projeto para "Aguardando parecer".
func (r *openRun) complete(ctx, fin context.Context) error {
	s := r.svc
	items, err := s.q.ListSigadocItems(ctx, r.projectID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Status != itemJoined {
			return &itemFailure{msg: "Há itens que ainda não foram inseridos no processo. Retome a montagem."}
		}
	}
	return database.WithTx(fin, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(fin, r.projectID)
		if err != nil {
			return err
		}
		if cur.Status != StatusSubmitted || str(cur.Stage) != StageOpeningSigadoc {
			return errCancelled
		}
		row, err := q.CompleteProjectOpening(fin, db.CompleteProjectOpeningParams{ID: r.projectID, UpdatedByID: &r.actor.ID})
		if err != nil {
			return err
		}
		if err := s.transition(fin, q, row, r.actor.entry(actionSigadocReady, r.projectID, map[string]any{"task": r.task.ID, "items": len(items)})); err != nil {
			return err
		}
		return q.FinishSigadocTask(fin, db.FinishSigadocTaskParams{ID: r.task.ID, Status: taskCompleted, ProgressDone: int32(len(items)), ProgressTotal: int32(len(items))}) //nolint:gosec // poucos itens
	})
}

func (r *openRun) setState(ctx context.Context, it db.SigadocItem, status, sigla, msg string) error {
	return r.svc.q.SetSigadocItemState(ctx, db.SetSigadocItemStateParams{ProjectID: r.projectID, Key: it.Key, Status: status, Sigla: sigla, Error: truncate(msg, 300)})
}

// advance leva um item até o fim, um passo por vez, gravando o estado logo depois de cada passo. content traz o PDF (nil para o Termo,
// que é criado do modelo). Chamado de novo (retomada), continua do passo em que parou.
func (r *openRun) advance(ctx context.Context, it db.SigadocItem, content func() ([]byte, string, error)) error {
	c := r.svc.sigadoc.Client()
	status, sigla := it.Status, it.Sigla
	if status == itemJoined {
		return nil
	}
	if status == itemFailed {
		status = itemPending
	}
	if status == itemPending || status == itemCreating {
		if err := r.active(ctx); err != nil {
			return err
		}
		warn := ""
		if status == itemCreating {
			// A tentativa anterior marcou "criando" e parou antes de gravar a sigla: o documento pode já existir no SIGADOC.
			found, ok, err := c.FindByDescription(ctx, r.tok, it.Description, r.svc.now().Add(-31*24*time.Hour))
			switch {
			case err == nil && ok:
				sigla, status = found, itemCreated
				if err := r.setState(ctx, it, itemCreated, sigla, ""); err != nil {
					return err
				}
			case errors.Is(err, sigadoc.ErrUnsupported):
				warn = "A tentativa anterior parou no meio da criação: confira no SIGADOC se não há um documento repetido."
			case err != nil:
				return err
			}
		}
		if status != itemCreated {
			if err := r.setState(ctx, it, itemCreating, "", warn); err != nil {
				return err
			}
			doc := sigadoc.NewDocument{Description: it.Description, Subscriber: r.task.Matricula, Electronic: true}
			switch it.Kind {
			case itemTermo:
				var interview map[string]string
				_ = json.Unmarshal(it.Payload, &interview)
				doc.Model, doc.Interview = r.settings.TermoModel, interview
				doc.Classification, doc.AccessLevel, doc.DestinationLotacao = r.settings.Classification, r.settings.AccessLevel, r.settings.DestinationLotacao
			default:
				pdf, name, err := content()
				if err != nil {
					_ = r.setState(ctx, it, itemFailed, "", itemErrorText(err))
					return &itemFailure{msg: it.Label + ": " + itemErrorText(err)}
				}
				doc.File, doc.Filename = pdf, name
				if it.Kind == itemReport {
					doc.Model, doc.DocType = r.settings.ReportModel, r.settings.ReportDocType
				} else {
					doc.Model, doc.DocType = r.settings.DocumentModel, r.settings.DocumentDocType
				}
				doc.Classification, doc.AccessLevel, doc.DestinationLotacao = r.settings.Classification, r.settings.AccessLevel, r.settings.DestinationLotacao
			}
			newSigla, err := c.CreateDocument(ctx, r.tok, doc)
			if err != nil {
				return err
			}
			sigla, status = newSigla, itemCreated
			if err := r.setState(ctx, it, itemCreated, sigla, warn); err != nil {
				return err
			}
		}
	}
	steps := []struct {
		from, to string
		do       func() error
	}{
		{itemCreated, itemFinalized, func() error {
			// A criação devolve um número provisório; o definitivo vem na finalização e é o que as chamadas seguintes usam.
			def, err := c.Finalize(ctx, r.tok, sigla)
			if err != nil {
				return err
			}
			sigla = def
			return nil
		}},
		{itemFinalized, itemSigned, func() error {
			// O Termo (produzido no SIGADOC) e o relatório (Interno Capturado) se assinam; os arquivos do proponente (Externo Capturado) se autenticam.
			if it.Kind == itemDocument {
				return c.AutenticarDocumento(ctx, r.tok, sigla, r.pin)
			}
			return c.Sign(ctx, r.tok, sigla, r.pin)
		}},
	}
	for _, st := range steps {
		if status != st.from {
			continue
		}
		if err := r.active(ctx); err != nil {
			return err
		}
		if err := st.do(); err != nil {
			return err
		}
		status = st.to
		if err := r.setState(ctx, it, status, sigla, ""); err != nil {
			return err
		}
	}
	if status == itemSigned {
		if err := r.active(ctx); err != nil {
			return err
		}
		if it.Kind == itemTermo {
			if err := r.autuar(ctx, sigla); err != nil {
				return err
			}
		} else {
			proc, err := r.svc.q.GetProjectSigadoc(ctx, r.projectID)
			if err != nil {
				return err
			}
			if err := c.Join(ctx, r.tok, sigla, proc.ProcessSigla); err != nil {
				return err
			}
		}
		if err := r.setState(ctx, it, itemJoined, sigla, ""); err != nil {
			return err
		}
	}
	r.touch(ctx)
	return nil
}

// autuar autua o Termo, descobre o número do processo e o grava no projeto (imutável).
func (r *openRun) autuar(ctx context.Context, termoSigla string) error {
	c := r.svc.sigadoc.Client()
	if _, err := r.svc.q.GetProjectSigadoc(ctx, r.projectID); err == nil {
		return nil // já registrado (retomada)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	id, err := c.Autuar(ctx, r.tok, termoSigla)
	if err != nil {
		return err
	}
	list, err := c.Movements(ctx, r.tok, termoSigla)
	if err != nil {
		return err
	}
	number, sigla := processRefs(list, id, termoSigla)
	return database.WithTx(ctx, r.svc.pool, func(tx pgx.Tx) error {
		return r.svc.RegisterSigadocProcess(ctx, r.svc.q.WithTx(tx), r.projectID, r.task.Matricula, number, sigla, &r.actor.ID)
	})
}

// processRefs escolhe o número (para mostrar) e a sigla (para as chamadas) do processo. A autuação cria um processo NOVO e devolve o
// número dele (`id`); a consulta de movimentações, ao contrário, é do documento consultado (o Termo, que fica dentro do processo), por
// isso só serve de reserva. Gravar o número do Termo como se fosse o do processo faz a juntada falhar.
func processRefs(list sigadoc.MovementList, autuarID, termoSigla string) (number, sigla string) {
	number = strings.TrimSpace(autuarID)
	if number == "" {
		number = strings.TrimSpace(list.Sigla)
	}
	if number == "" {
		number = termoSigla
	}
	return number, apiSigla(number)
}

// apiSigla é a sigla como as chamadas a pedem: sem traços, barras nem espaços.
func apiSigla(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return strings.ToUpper(b.String())
}

func itemErrorText(err error) string {
	var itf *itemFailure
	switch {
	case errors.As(err, &itf):
		return itf.msg
	case errors.Is(err, pdfbuild.ErrPageTooLarge):
		return "Uma página sozinha passa do tamanho máximo de cada documento no SIGADOC."
	}
	return reasonOf(err)
}
