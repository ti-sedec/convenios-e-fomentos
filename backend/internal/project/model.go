// Pacote project é o cadastro de projetos de convênio e de termo de fomento feito pelo proponente.
// Nesta fase existe o rascunho, em três seções salvas separadamente: Proponente, Instituição e Proposta.
package project

import (
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// Nomes de recurso e ações no motor de permissões.
const (
	Resource        = "project"
	ProfileResource = "proponent-profile"

	ActionCreate = "admin::projects.create"
	ActionRead   = "admin::projects.read"
	ActionUpdate = "admin::projects.update"
	ActionDelete = "admin::projects.delete"
	ActionSubmit = "admin::projects.submit"
	// Ações do ciclo de vida depois do envio. Withdraw é do proponente (desistência); as demais são da equipe.
	ActionWithdraw          = "admin::projects.withdraw"
	ActionRequestComplement = "admin::projects.request-complement"
	ActionArchive           = "admin::projects.archive"
	// Conferência da análise (Analista): assumir a análise e aprovar/desfazer itens.
	ActionStartAnalysis = "admin::projects.start-analysis"
	ActionReviewItem    = "admin::projects.review-item"
	// Enquadramento: o Analista indica o departamento; o Revisor do Departamento confirma ou recusa.
	ActionAssignDepartment = "admin::projects.assign-department"
	ActionReviewFraming    = "admin::projects.review-framing"
	ActionDeny             = "admin::projects.deny"

	ActionProfileRead   = "admin::proponent-profile.read"
	ActionProfileUpdate = "admin::proponent-profile.update"
)

// Instrumentos, situações e demais valores fixos.
const (
	InstrumentConvenio = "convenio"
	InstrumentFomento  = "fomento"

	StatusDraft     = "draft"
	StatusSubmitted = "submitted"
	StatusArchived  = "archived"

	// StageAwaitingChecklist é a primeira etapa do fluxo da equipe: aguardando a conferência do checklist.
	StageAwaitingChecklist = "awaiting_checklist"
	StageInAnalysis        = "in_analysis"
	// StageAwaitingProponent: a equipe devolveu itens e o proponente tem prazo para corrigi-los.
	StageAwaitingProponent = "awaiting_proponent"
	// StageAwaitingOpinion: o Revisor do Departamento confirmou o enquadramento; falta o parecer técnico (fora deste fluxo).
	StageAwaitingOpinion = "awaiting_opinion"

	// Situação do enquadramento em andamento (nulo = nenhum) e decisão de cada rodada.
	FramingPending   = "pending"
	FramingConfirmed = "confirmed"
	FramingRefused   = "refused"

	// Motivos de arquivamento.
	ArchiveComplementDeadline = "complement_deadline"
	ArchiveWithdrawn          = "withdrawn"
	ArchiveDenied             = "denied"
	ArchiveMissingDocuments   = "missing_documents"
	ArchiveOther              = "other"

	HabilitationFull    = "full"
	HabilitationPartial = "partial"

	PhoneMobile   = "mobile"
	PhoneLandline = "landline"

	DateLayout = time.DateOnly
)

// Ref são os campos do projeto que o motor de permissões enxerga: é o que as condições
// (`admin::is-creator`, `admin::same-department`) usam para filtrar. Os demais dados do projeto não
// participam de permissões, então o Schema só descreve estes.
type Ref struct {
	ID           uuid.UUID  `json:"id" db:"id" authz:"system"`
	Status       string     `json:"status" db:"status" authz:"system"`
	DepartmentID *uuid.UUID `json:"department_id" db:"department_id"`
	CreatedByID  uuid.UUID  `json:"created_by_id" db:"created_by_id" authz:"system"`
}

// Getter expõe os campos para avaliar filtros de permissão em memória.
func (r Ref) Getter() authz.Getter {
	m := map[string]any{"id": r.ID, "status": r.Status, "department_id": r.DepartmentID, "created_by_id": r.CreatedByID}
	return func(field string) (any, bool) { v, ok := m[field]; return v, ok }
}

// Schema descreve os campos de permissão do projeto.
var Schema = func() *authz.Schema {
	s, err := authz.NewSchema(Resource, Ref{})
	if err != nil {
		panic(err)
	}
	return s
}()

// Register registra as ações e o schema no motor de permissões.
func Register(reg *authz.Registry) {
	proj := func(id, name string, conds bool) authz.Action {
		return authz.Action{ID: id, Domain: authz.DomainAdmin, Category: authz.CategoryCollectionTypes, Name: name, Resource: Resource, SupportsConditions: conds}
	}
	profile := func(id, name string) authz.Action {
		return authz.Action{ID: id, Domain: authz.DomainAdmin, Category: authz.CategoryCollectionTypes, Name: name, Resource: ProfileResource}
	}
	reg.MustRegister(
		proj(ActionCreate, "Criar projetos", false),
		proj(ActionRead, "Ler projetos", true),
		proj(ActionUpdate, "Atualizar projetos", true),
		proj(ActionDelete, "Excluir projetos", true),
		proj(ActionSubmit, "Enviar projetos", true),
		proj(ActionWithdraw, "Desistir de projetos enviados", true),
		proj(ActionRequestComplement, "Pedir complementação de projetos", true),
		proj(ActionArchive, "Arquivar projetos (falta de documentos ou outro motivo)", true),
		proj(ActionStartAnalysis, "Assumir a análise de projetos enviados", true),
		proj(ActionReviewItem, "Aprovar ou desfazer a aprovação de itens na conferência da análise", true),
		proj(ActionAssignDepartment, "Indicar o departamento de enquadramento de um projeto", true),
		proj(ActionReviewFraming, "Confirmar ou recusar o enquadramento de um projeto no departamento", true),
		proj(ActionDeny, "Negar projetos (decisão do Secretário)", true),
		proj(ActionShare, "Gerir o acesso aos projetos (convidar por CPF e revogar)", true),
		profile(ActionProfileRead, "Ler o próprio perfil de proponente"),
		profile(ActionProfileUpdate, "Atualizar o próprio perfil de proponente"),
	)
	reg.RegisterSchema(Resource, Schema)
}

// ---- Dados trocados com a API ----

// Phone é um telefone de contato: só dígitos com DDD (10 ou 11).
type Phone struct {
	Number string `json:"number"`
	Type   string `json:"type"` // mobile | landline
}

// Email é um e-mail de contato.
type Email struct {
	Address string `json:"address"`
	Primary bool   `json:"primary"`
}

// Contact reúne os dados de contato e de localização do proponente, comuns ao perfil e ao projeto.
type Contact struct {
	Phones           []Phone `json:"phones"`
	Emails           []Email `json:"emails"`
	UF               string  `json:"uf"`
	MunicipalityIBGE string  `json:"municipalityIbge"`
	JobTitle         string  `json:"jobTitle"`
	// Personal são os dados pessoais de quem assina as declarações, quando o titular do perfil é o próprio signatário.
	Personal PersonalData `json:"personal"`
}

// Profile é o perfil do proponente. Nome e CPF vêm do MT Login e não são editáveis.
type Profile struct {
	Name string `json:"name"`
	CPF  string `json:"cpf"`
	Contact
	// Complete indica que o perfil já tem o mínimo para iniciar um projeto sem pendências de contato.
	Complete bool `json:"complete"`
}

// ProponentSection é a seção "Proponente" do projeto. Name e CPF são de leitura (vêm do MT Login).
type ProponentSection struct {
	Name string `json:"name"`
	CPF  string `json:"cpf"`
	Contact
	IsLegalRepresentative *bool  `json:"isLegalRepresentative"`
	LegalRepName          string `json:"legalRepName"`
	LegalRepCPF           string `json:"legalRepCpf"`
	LegalRepJobTitle      string `json:"legalRepJobTitle"`
	// LegalRepPersonal são os dados pessoais do representante legal (quando é outra pessoa).
	LegalRepPersonal PersonalData `json:"legalRepPersonal"`
}

// InstitutionSection é a seção "Instituição". Nature e NonProfit são derivados do tipo de entidade,
// exceto no tipo "Outro", em que o proponente os informa.
type InstitutionSection struct {
	Instrument   string `json:"instrument"`
	CNPJ         string `json:"cnpj"`
	Name         string `json:"name"`
	EntityTypeID string `json:"entityTypeId"`
	// EntityTypeCode e EntityTypeIsOther (somente leitura) identificam o tipo escolhido; "Outro" pede o texto livre e a natureza.
	EntityTypeCode    string `json:"entityTypeCode"`
	EntityTypeIsOther bool   `json:"entityTypeIsOther"`
	EntityTypeOther   string `json:"entityTypeOther"`
	Nature            string `json:"nature"`
	NonProfit         bool   `json:"nonProfit"`
	UF                string `json:"uf"`
	MunicipalityIBGE  string `json:"municipalityIbge"`
	Zip               string `json:"zip"`
	Street            string `json:"street"`
	Number            string `json:"number"`
	Complement        string `json:"complement"`
	District          string `json:"district"`
	Email             string `json:"email"`
	Phone             string `json:"phone"`

	HabilitationStatus     string `json:"habilitationStatus"`
	HabilitationNumber     string `json:"habilitationNumber"`
	HabilitationValidUntil string `json:"habilitationValidUntil"`
}

// Municipality é um município de execução.
type Municipality struct {
	IBGECode string `json:"ibgeCode"`
	Name     string `json:"name"`
	UF       string `json:"uf"`
}

// ProposalSection é a seção "Proposta". Na gravação, Municipalities é a lista de códigos IBGE
// (MunicipalityCodes); na leitura, vêm os municípios completos.
type ProposalSection struct {
	SigconNumber         string `json:"sigconNumber"`
	SigconRegisteredOn   string `json:"sigconRegisteredOn"`
	Title                string `json:"title"`
	Object               string `json:"object"`
	Justification        string `json:"justification"`
	StartDate            string `json:"startDate"`
	EndDate              string `json:"endDate"`
	ObjectNature         string `json:"objectNature"`
	HasArtisticHiring    bool   `json:"hasArtisticHiring"`
	Calamity             bool   `json:"calamity"`
	ExecutionPlaceDetail string `json:"executionPlaceDetail"`

	MunicipalityCodes []string       `json:"municipalityCodes,omitempty"`
	Municipalities    []Municipality `json:"municipalities"`
}

// Amendment é uma emenda parlamentar do projeto. Na gravação, informe ParliamentarianID (do cadastro) ou, se o parlamentar
// não estiver nele, ParliamentarianName por extenso. Na leitura, ParliamentarianName traz o nome em qualquer dos casos.
type Amendment struct {
	// ID é estável: reenvie o ID de uma emenda existente para mantê-la (e os documentos anexados a ela).
	ID                  string `json:"id"`
	ParliamentarianID   string `json:"parliamentarianId"`
	ParliamentarianName string `json:"parliamentarianName"`
	Number              string `json:"number"`
	Year                int    `json:"year"`
	AmountCents         int64  `json:"amountCents"`
}

// FundingSection é a seção "Recursos". Todos os valores são em centavos. AmendmentTotalCents e TotalCents são calculados
// pelo servidor (somente leitura): total solicitado = recurso próprio da SEDEC + soma das emendas.
type FundingSection struct {
	Type                string      `json:"type"` // amendment | own | mixed
	OwnAmountCents      int64       `json:"ownAmountCents"`
	Amendments          []Amendment `json:"amendments"`
	AmendmentTotalCents int64       `json:"amendmentTotalCents"`
	TotalCents          int64       `json:"totalCents"`

	CounterpartKind        string `json:"counterpartKind"` // financial | in_kind
	CounterpartCents       int64  `json:"counterpartCents"`
	CounterpartDescription string `json:"counterpartDescription"`
	IndirectCostsCents     int64  `json:"indirectCostsCents"`

	TenderStatus        string `json:"tenderStatus"` // held | waived | unfeasible | not_applicable
	TenderNoticeNumber  string `json:"tenderNoticeNumber"`
	TenderApprovedOn    string `json:"tenderApprovedOn"`
	TenderLegalBasis    string `json:"tenderLegalBasis"`
	TenderJustification string `json:"tenderJustification"`

	ArtistFeesCents         int64  `json:"artistFeesCents"`
	RegionalArtistFeesCents int64  `json:"regionalArtistFeesCents"`
	GovernorWaiver          bool   `json:"governorWaiver"`
	PriceOldestCollectedOn  string `json:"priceOldestCollectedOn"`
}

// Issue é uma pendência do projeto: algo que impede o envio (block) ou merece atenção (warn).
type Issue struct {
	Section  string `json:"section"` // proponent | institution | proposal | funding | documents
	Field    string `json:"field"`
	Code     string `json:"code"`
	Severity string `json:"severity"` // block | warn
	Message  string `json:"message"`
}

// Project é o projeto completo, com as seções e as pendências.
type Project struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Version int32     `json:"version"`
	IsOwner bool      `json:"isOwner"`
	// Access é a relação de quem consulta com o projeto: owner (titular), edit ou read (membros); vazio para a equipe.
	Access string `json:"access"`
	// Code, Stage, SubmittedAt e LateSubmission só existem depois do envio.
	Code           string     `json:"code"`
	Stage          string     `json:"stage"`
	SubmittedAt    *time.Time `json:"submittedAt"`
	LateSubmission bool       `json:"lateSubmission"`
	// Complement é o pedido de complementação aberto ou, não havendo, o mais recente. Archive só existe em projeto arquivado.
	Complement *Complement  `json:"complement"`
	Archive    *ArchiveInfo `json:"archive"`
	// Analyst e Review só vão para a equipe (a conferência é interna). Analyst some enquanto ninguém assumiu a análise.
	Analyst *AnalystInfo `json:"analyst,omitempty"`
	Review  *Review      `json:"review,omitempty"`
	// Framing é a rodada de enquadramento em curso (pendente ou confirmada); Framings é o histórico, da mais antiga à mais nova.
	Framing         *Framing  `json:"framing,omitempty"`
	Framings        []Framing `json:"framings,omitempty"`
	OriginProjectID string    `json:"originProjectId"`
	OriginCode      string    `json:"originCode"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`

	Proponent   ProponentSection   `json:"proponent"`
	Institution InstitutionSection `json:"institution"`
	Proposal    ProposalSection    `json:"proposal"`
	Funding     FundingSection     `json:"funding"`
	// DocumentOptions são as situações informadas que acrescentam documentos; Documents resume o checklist.
	DocumentOptions DocumentOptions `json:"documentOptions"`
	Documents       DocumentSummary `json:"documents"`
	Issues          []Issue         `json:"issues"`
}

// ComplementItem é algo que a equipe devolveu: uma seção (Kind "section"), um documento do checklist (Kind "document") ou um documento
// adicional do proponente (Kind "extra").
// Target é o identificador: o nome da seção (proponent, institution, proposal, funding, document-options) ou o documento
// ("codigo" ou "codigo@id-da-emenda") ou o id do documento adicional. Answered indica que o proponente já mexeu no item depois do pedido.
type ComplementItem struct {
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Label    string `json:"label"`
	Note     string `json:"note"`
	Answered bool   `json:"answered"`
}

// Complement é um pedido de complementação.
type Complement struct {
	Number      int              `json:"number"`
	Status      string           `json:"status"` // open | answered | expired | cancelled
	RequestedAt time.Time        `json:"requestedAt"`
	DueAt       time.Time        `json:"dueAt"`
	Overdue     bool             `json:"overdue"`
	Message     string           `json:"message"`
	Items       []ComplementItem `json:"items"`
}

// ArchiveInfo descreve o arquivamento.
type ArchiveInfo struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
	Note   string    `json:"note"`
}

// Summary é a linha da listagem de projetos.
type Summary struct {
	ID              uuid.UUID  `json:"id"`
	Status          string     `json:"status"`
	Version         int32      `json:"version"`
	Instrument      string     `json:"instrument"`
	Title           string     `json:"title"`
	InstitutionName string     `json:"institutionName"`
	InstitutionCNPJ string     `json:"institutionCnpj"`
	SigconNumber    string     `json:"sigconNumber"`
	Code            string     `json:"code"`
	Stage           string     `json:"stage"`
	ArchiveReason   string     `json:"archiveReason"`
	ComplementDueAt *time.Time `json:"complementDueAt"`
	SubmittedAt     *time.Time `json:"submittedAt"`
	// Department, FramingStatus e Analyst só vão para a equipe (são internos); o titular e os membros não os recebem.
	Department    *DepartmentRef `json:"department,omitempty"`
	FramingStatus string         `json:"framingStatus,omitempty"`
	Analyst       *AnalystInfo   `json:"analyst,omitempty"`
	// StageSeconds é há quanto tempo (horas corridas) o projeto enviado está na etapa atual. Só a equipe recebe.
	StageSeconds *int64    `json:"stageSeconds,omitempty"`
	TotalCents   int64     `json:"totalCents"`
	IsOwner      bool      `json:"isOwner"`
	Access       string    `json:"access"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
