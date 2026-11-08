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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/catalog"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cnpj"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mtid"
	"github.com/wagner/convenios-e-fomentos/backend/internal/preview"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
	"github.com/wagner/convenios-e-fomentos/backend/internal/scan"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
)

// Actor é o administrador que age sobre o projeto. Nome e CPF vêm do MT Login.
type Actor struct {
	ID   uuid.UUID
	CPF  string
	Name string
	IP   string
}

func (a Actor) entry(action string, id uuid.UUID, payload map[string]any) audit.Entry {
	return audit.Entry{ActorType: "admin", ActorID: a.ID.String(), Action: action, Entity: "project", EntityID: id.String(), Payload: payload, IP: a.IP}
}

// Service implementa o cadastro de projetos.
type Service struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	audit audit.Recorder
	rules *rules.Service
	now   func() time.Time

	// Armazenamento dos arquivos (opcional; ver SetStorage).
	store       storage.Store
	uploadTTL   time.Duration
	downloadTTL time.Duration
	log         *slog.Logger

	// Exibição dos documentos no navegador (opcional; ver SetPreview).
	converter  preview.Converter
	previewTTL time.Duration

	// Verificação de vírus dos arquivos (opcional; ver SetScanner).
	scanner scan.Scanner

	// MT-ID, que assina as declarações geradas pelo sistema (opcional; ver SetMTID).
	mtid mtid.Client

	// Candidatos do bloco "pronto para indicar" da visão geral da fila (ver SetReadyCandidates); zero usa o padrão.
	readyCandidates int

	// Link dos e-mails para o painel (ver SetNotifier). Os e-mails vão para a caixa de saída (internal/outbox), nunca direto ao SMTP.
	publicURL string
}

// NewService cria o serviço. Os parâmetros das regras (limites, prazos) vêm do serviço rules.
func NewService(pool *pgxpool.Pool, rules *rules.Service) *Service {
	return &Service{pool: pool, q: db.New(pool), rules: rules, now: time.Now}
}

// today é a data de hoje no fuso de Cuiabá, à meia-noite UTC (para comparar com colunas date).
func (s *Service) today() time.Time {
	loc, err := time.LoadLocation("America/Cuiaba")
	if err != nil {
		loc = time.FixedZone("-04", -4*3600)
	}
	n := s.now().In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpapi.ErrNotFound()
	}
	return err
}

func errNotEditable() error {
	return httpapi.ErrConflict("project_not_editable", "Este projeto não pode mais ser alterado.")
}

func errVersionConflict(current int32) error {
	e := httpapi.ErrConflict("project_version_conflict", "Este projeto foi alterado por outra pessoa ou em outra janela. Recarregue para ver a versão mais recente.")
	e.Details = map[string]any{"currentVersion": current}
	return e
}

// ---- Contato (perfil e seção Proponente) ----

type contactOut struct {
	phones, emails []byte
	uf             pgtype.Text
	ibge           *string
	job            string
	personal       []byte
}

func normalizeContact(ctx context.Context, q *db.Queries, in Contact, p *problems) contactOut {
	out := contactOut{job: text(in.JobTitle)}
	ph := phones(in.Phones, "phones", p)
	em := emails(in.Emails, "emails", p)
	out.phones, _ = json.Marshal(ph)
	out.emails, _ = json.Marshal(em)
	out.uf, out.ibge = place(ctx, q, in.UF, in.MunicipalityIBGE, "uf", "municipalityIbge", p)
	if tooLong(out.job, maxJobTitle) {
		p.add("jobTitle", "Use até 100 caracteres.")
	}
	out.personal, _ = normalizePersonal(ctx, q, in.Personal, "personal", p)
	return out
}

func contactOf(phonesJSON, emailsJSON []byte, uf pgtype.Text, ibge *string, job string, personal []byte) Contact {
	c := Contact{Phones: []Phone{}, Emails: []Email{}, UF: uf.String, MunicipalityIBGE: str(ibge), JobTitle: job, Personal: personalOf(personal)}
	_ = json.Unmarshal(phonesJSON, &c.Phones)
	_ = json.Unmarshal(emailsJSON, &c.Emails)
	if c.Phones == nil {
		c.Phones = []Phone{}
	}
	if c.Emails == nil {
		c.Emails = []Email{}
	}
	return c
}

func contactComplete(c Contact) bool {
	for _, ph := range c.Phones {
		if ph.Type == PhoneMobile && len(c.Emails) > 0 && c.MunicipalityIBGE != "" && c.JobTitle != "" {
			return true
		}
	}
	return false
}

// GetProfile devolve o perfil do proponente; sem perfil salvo, devolve um vazio com nome e CPF do login.
func (s *Service) GetProfile(ctx context.Context, actor Actor) (*Profile, error) {
	row, err := s.q.GetProponentProfile(ctx, actor.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &Profile{Name: actor.Name, CPF: actor.CPF, Contact: contactOf(nil, nil, pgtype.Text{}, nil, "", nil)}, nil
	}
	if err != nil {
		return nil, err
	}
	return profileView(actor, row), nil
}

func profileView(actor Actor, row db.ProponentProfile) *Profile {
	c := contactOf(row.Phones, row.Emails, row.Uf, row.MunicipalityIbge, row.JobTitle, row.Personal)
	return &Profile{Name: actor.Name, CPF: actor.CPF, Contact: c, Complete: contactComplete(c)}
}

// SaveProfile grava o perfil do proponente (a validação é a mesma dos dados de contato do projeto).
func (s *Service) SaveProfile(ctx context.Context, actor Actor, in Contact) (*Profile, error) {
	var p problems
	c := normalizeContact(ctx, s.q, in, &p)
	if err := p.err(); err != nil {
		return nil, err
	}
	row, err := s.q.UpsertProponentProfile(ctx, db.UpsertProponentProfileParams{AdminID: actor.ID, Phones: c.phones, Emails: c.emails, Uf: c.uf, MunicipalityIbge: c.ibge, JobTitle: c.job, Personal: c.personal})
	if err != nil {
		return nil, err
	}
	return profileView(actor, row), nil
}

// ---- Leitura ----

func refOf(r db.Project) Ref {
	return Ref{ID: r.ID, Status: r.Status, DepartmentID: r.DepartmentID, CreatedByID: r.CreatedByID}
}

func (s *Service) view(ctx context.Context, q *db.Queries, r db.Project, actor Actor) (*Project, error) {
	p, cl, err := s.viewWithChecklist(ctx, q, r, actor)
	if err != nil {
		return nil, err
	}
	return p, s.attachReview(ctx, q, r, p, cl, actor)
}

// viewWithChecklist monta o projeto e, junto, o checklist de documentos (já calculado para o resumo e as pendências).
func (s *Service) viewWithChecklist(ctx context.Context, q *db.Queries, r db.Project, actor Actor) (*Project, *Checklist, error) {
	periods, err := q.ListProjectExecutionPeriods(ctx, r.ID)
	if err != nil {
		return nil, nil, err
	}
	isOther, typeCode := false, ""
	if r.EntityTypeID != nil {
		t, err := q.GetEntityType(ctx, *r.EntityTypeID)
		if err != nil {
			return nil, nil, err
		}
		isOther, typeCode = t.Code == catalog.OtherCode, t.Code
	}
	p := Project{
		ID: r.ID, Status: r.Status, Version: r.Version, IsOwner: r.CreatedByID == actor.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		Code: str(r.Code), Stage: str(r.Stage), SubmittedAt: r.SubmittedAt, LateSubmission: r.LateSubmission,
		Proponent: ProponentSection{
			Name: r.ProponentName, CPF: r.ProponentCpf,
			Contact:               contactOf(r.ProponentPhones, r.ProponentEmails, r.ProponentUf, r.ProponentMunicipalityIbge, r.ProponentJobTitle, r.ProponentPersonal),
			IsLegalRepresentative: boolPtr(r.IsLegalRepresentative),
			LegalRepName:          r.LegalRepName, LegalRepCPF: str(r.LegalRepCpf), LegalRepJobTitle: r.LegalRepJobTitle,
			LegalRepPersonal: personalOf(r.LegalRepPersonal),
		},
		Institution: InstitutionSection{
			Instrument: str(r.Instrument), CNPJ: str(r.InstitutionCnpj), Name: r.InstitutionName, EntityTypeID: uuidString(r.EntityTypeID),
			EntityTypeCode: typeCode, EntityTypeIsOther: isOther, EntityTypeOther: r.EntityTypeOther, Nature: str(r.EntityNature), NonProfit: r.EntityNonProfit,
			UF: r.InstitutionUf.String, MunicipalityIBGE: str(r.InstitutionMunicipalityIbge), Zip: r.InstitutionZip, Street: r.InstitutionStreet,
			Number: r.InstitutionNumber, Complement: r.InstitutionComplement, District: r.InstitutionDistrict, Email: r.InstitutionEmail,
			Phone: r.InstitutionPhone, HabilitationStatus: str(r.HabilitationStatus), HabilitationNumber: r.HabilitationNumber,
			HabilitationValidUntil: dateString(r.HabilitationValidUntil),
		},
		Proposal: ProposalSection{
			SigconNumber: str(r.SigconNumber), SigconRegisteredOn: dateString(r.SigconRegisteredOn), Title: r.Title, Object: r.ObjectDescription,
			Justification: r.Justification, ObjectNature: str(r.ObjectNature),
			HasArtisticHiring: r.HasArtisticHiring, Calamity: r.Calamity,
		},
	}
	p.Proposal.ExecutionPeriods, p.Proposal.StartDate, p.Proposal.EndDate, p.Proposal.Municipalities = executionPeriodsView(periods)
	access, err := s.accessOf(ctx, q, r.ID, r.CreatedByID, actor)
	if err != nil {
		return nil, nil, err
	}
	p.Access = access
	// Minimização: o CPF completo só aparece para o titular e para os membros que ele convidou; a equipe vê o CPF mascarado.
	if access == "" {
		p.Proponent.CPF = maskCPF(p.Proponent.CPF)
		p.Proponent.LegalRepCPF = maskCPF(p.Proponent.LegalRepCPF)
	}
	dup := false
	if r.SigconNumber != nil {
		n, err := q.CountOtherProjectsWithSigconNumber(ctx, db.CountOtherProjectsWithSigconNumberParams{SigconNumber: r.SigconNumber, ID: r.ID})
		if err != nil {
			return nil, nil, err
		}
		dup = n > 0
	}
	if p.Funding, err = s.fundingView(ctx, q, r); err != nil {
		return nil, nil, err
	}
	snap, err := s.rules.SnapshotAt(ctx, q, s.today())
	if err != nil {
		return nil, nil, err
	}
	p.OriginProjectID = uuidString(r.OriginProjectID)
	p.OriginCode = s.originView(ctx, q, r)
	if r.Status == StatusArchived && r.ArchivedAt != nil {
		p.Archive = &ArchiveInfo{At: *r.ArchivedAt, Reason: str(r.ArchiveReason), Note: r.ArchiveNote}
	}
	p.DocumentOptions = DocumentOptions{ExclusiveSupplier: r.ExclusiveSupplier, PublicSpaceUse: r.PublicSpaceUse, ReducedDeadline: r.ReducedDeadline}
	cl, err := s.checklist(ctx, q, p)
	if err != nil {
		return nil, nil, err
	}
	if err := s.markStaleDeclarations(ctx, q, r, &p, cl); err != nil {
		return nil, nil, err
	}
	p.Documents = cl.Summary
	if p.Complement, err = s.complementView(ctx, q, r, cl); err != nil {
		return nil, nil, err
	}
	p.Issues = computeIssues(p, dup, s.today(), snap)
	if di := documentIssue(cl.Summary); di != nil {
		p.Issues = append(p.Issues, *di)
	}
	if r.Status == StatusDraft {
		if iss := s.deadlineIssue(snap); iss != nil {
			p.Issues = append(p.Issues, *iss)
		}
	}
	return &p, cl, nil
}

func boolPtr(b pgtype.Bool) *bool {
	if !b.Valid {
		return nil
	}
	return &b.Bool
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// Get devolve o projeto se a decisão de leitura o permite; caso contrário 404 (não confirma que existe).
func (s *Service) Get(ctx context.Context, actor Actor, id uuid.UUID, read authz.Decision) (*Project, error) {
	s.settle(ctx, id)
	r, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if !read.AllowsRecord(refOf(r).Getter()) {
		return nil, httpapi.ErrNotFound()
	}
	s.settleDeclarations(ctx, id) // só depois de conferir o acesso: quem não vê o projeto não dispara consulta ao MT-ID
	v, err := s.view(ctx, s.q, r, actor)
	if err == nil && v.Access != "" && v.Access != AccessOwner {
		s.markAccepted(ctx, id, actor)
	}
	return v, err
}

// ListParams são os parâmetros da listagem.
type ListParams struct {
	Perm   authz.Filter // restrição vinda da permissão de leitura
	Actor  Actor
	Q      string
	Status string
	// Access restringe a listagem ao que a pessoa tem como titular (AccessFilterOwner), como membro (AccessFilterMember) ou um dos dois
	// (AccessFilterAny); vazio não restringe além da permissão. É sempre somado ao filtro de permissão, nunca o substitui.
	Access string
	// Filtros da equipe (filas): etapas, situação do enquadramento (pending, confirmed ou none), departamento e analista.
	Stages     []string
	Framing    string
	Department *uuid.UUID
	Analyst    *uuid.UUID
	// IdleDays, quando maior que zero, só deixa os projetos enviados que estão na etapa atual há pelo menos tantos dias corridos.
	IdleDays int
	Sort     httpapi.Sort
	Page     httpapi.Page
}

// Valores de ListParams.Access.
const (
	AccessFilterOwner  = "owner"  // só os projetos de que a pessoa é titular
	AccessFilterMember = "member" // só os projetos em que a pessoa é membro ativo (e não titular)
	AccessFilterAny    = "all"    // titular ou membro ativo: o que é "dela", mesmo para quem tem leitura ampla
)

// accessCondition é a condição SQL de ListParams.Access. membership são os projetos em que a pessoa é membro ativo.
func accessCondition(access string, actor Actor, membership map[uuid.UUID]string, arg func(any) string) string {
	// Só registra o argumento de quem a condição usa: um argumento sobrando faz o Postgres recusar a consulta.
	owner := func() string { return "created_by_id = " + arg(actor.ID) }
	member := func() string {
		ids := make([]string, 0, len(membership))
		for id := range membership {
			ids = append(ids, id.String())
		}
		return "id = ANY(" + arg(ids) + "::uuid[])"
	}
	switch access {
	case AccessFilterOwner:
		return owner()
	case AccessFilterMember:
		m := member()
		return "(" + m + " AND NOT " + owner() + ")"
	case AccessFilterAny:
		o := owner()
		return "(" + o + " OR " + member() + ")"
	}
	return ""
}

// scopeConds são as condições que toda consulta de projetos da pessoa começa por: o filtro da permissão de leitura e, se pedido, o de
// acesso. Os argumentos de cada uma já vêm numerados ($1...); quem continua a consulta acrescenta os seus depois dos devolvidos.
// Devolve também os projetos em que a pessoa é membro ativo.
func (s *Service) scopeConds(ctx context.Context, perm authz.Filter, actor Actor, access string) (conds []string, args []any, member map[uuid.UUID]string, err error) {
	if !perm.IsTrue() {
		clause, pargs, err := perm.ToSQL(Schema.Column, 1)
		if err != nil {
			return nil, nil, nil, err
		}
		args = append(args, pargs...)
		conds = append(conds, clause)
	}
	if member, err = s.memberships(ctx, actor); err != nil {
		return nil, nil, nil, err
	}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if c := accessCondition(access, actor, member, arg); c != "" {
		conds = append(conds, c)
	}
	return conds, args, member, nil
}

// stageSinceSQL é desde quando um projeto enviado está na etapa atual, pelo histórico de etapas: o início dos trechos seguidos que têm
// a etapa de hoje (um enquadramento pendente continua na etapa de análise). Nulo fora de projeto enviado.
const stageSinceSQL = `(CASE WHEN projects.status = 'submitted' THEN (SELECT min(h.entered_at) FROM project_stage_history h
	WHERE h.project_id = projects.id AND h.seq > COALESCE((SELECT max(h2.seq) FROM project_stage_history h2
	WHERE h2.project_id = projects.id AND h2.stage <> projects.stage), 0)) END)`

// SortColumns é a lista permitida de ordenação. stage_since:asc põe primeiro quem está há mais tempo na etapa atual (e por último o
// que não é projeto enviado).
var SortColumns = map[string]string{"updated_at": "updated_at", "created_at": "created_at", "title": "title", "institution_name": "institution_name",
	"stage_since": "COALESCE(" + stageSinceSQL + ", 'infinity'::timestamptz)"}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List lista os projetos combinando SEMPRE o filtro de permissão com os filtros do usuário.
func (s *Service) List(ctx context.Context, p ListParams) ([]Summary, int, error) {
	conds, args, member, err := s.scopeConds(ctx, p.Perm, p.Actor, p.Access)
	if err != nil {
		return nil, 0, err
	}
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if p.Status != "" {
		conds = append(conds, "status = "+arg(p.Status))
	}
	if len(p.Stages) > 0 {
		conds = append(conds, "stage = ANY("+arg(p.Stages)+")")
	}
	switch p.Framing {
	case "":
	case "none":
		conds = append(conds, "framing_status IS NULL")
	default:
		conds = append(conds, "framing_status = "+arg(p.Framing))
	}
	if p.Department != nil {
		conds = append(conds, "department_id = "+arg(*p.Department))
	}
	if p.Analyst != nil {
		conds = append(conds, "analyst_id = "+arg(*p.Analyst))
	}
	if p.IdleDays > 0 {
		conds = append(conds, stageSinceSQL+" <= now() - make_interval(days => "+arg(p.IdleDays)+")")
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		like := arg("%" + escapeLike(q) + "%")
		cond := fmt.Sprintf("(title ILIKE %[1]s OR institution_name ILIKE %[1]s OR sigcon_number ILIKE %[1]s", like)
		// O termo, sem pontuação e em maiúsculas, é prefixo do CNPJ (numérico ou alfanumérico). Se tiver algum caractere que não cabe
		// num CNPJ, o prefixo vem só dos dígitos.
		k, ok := cnpj.Key(q)
		if !ok {
			k = digits(q)
		}
		if k != "" {
			cond += " OR institution_cnpj LIKE " + arg(escapeLike(k)+"%")
		}
		conds = append(conds, cond+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM projects"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	query := fmt.Sprintf(`SELECT id, status, version, COALESCE(instrument, ''), title, institution_name, COALESCE(institution_cnpj, ''),
		COALESCE(sigcon_number, ''), total_amount_cents, COALESCE(code, ''), submitted_at, COALESCE(stage, ''), COALESCE(archive_reason, ''),
		(SELECT due_at FROM project_complement_requests c WHERE c.project_id = projects.id AND c.status = 'open'), created_by_id, created_at, updated_at,
		department_id, (SELECT d.name FROM departments d WHERE d.id = projects.department_id), COALESCE(framing_status, ''),
		analyst_id, (SELECT NULLIF(trim(a.first_name || ' ' || a.last_name), '') FROM admins a WHERE a.id = projects.analyst_id),
		EXTRACT(EPOCH FROM now() - %[1]s)::bigint, late_submission
		FROM projects%[2]s ORDER BY %[3]s, id LIMIT %[4]d OFFSET %[5]d`,
		stageSinceSQL, where, p.Sort.SQL(), p.Page.PageSize, p.Page.Offset())
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		var x Summary
		var owner uuid.UUID
		var depID, anID *uuid.UUID
		var depName, anName *string
		var idle *int64
		if err := rows.Scan(&x.ID, &x.Status, &x.Version, &x.Instrument, &x.Title, &x.InstitutionName, &x.InstitutionCNPJ, &x.SigconNumber, &x.TotalCents, &x.Code, &x.SubmittedAt, &x.Stage, &x.ArchiveReason, &x.ComplementDueAt, &owner, &x.CreatedAt, &x.UpdatedAt,
			&depID, &depName, &x.FramingStatus, &anID, &anName, &idle, &x.LateSubmission); err != nil {
			return nil, 0, err
		}
		x.IsOwner = owner == p.Actor.ID
		if x.IsOwner {
			x.Access = AccessOwner
		} else {
			x.Access = member[x.ID]
		}
		if x.Access == "" { // equipe: os campos internos
			if depID != nil {
				x.Department = &DepartmentRef{ID: depID, Name: str(depName)}
			}
			if anID != nil {
				x.Analyst = &AnalystInfo{ID: *anID, Name: str(anName), IsMe: *anID == p.Actor.ID}
			}
			x.StageSeconds = idle
		} else {
			x.FramingStatus = ""
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

// ---- Escrita ----

// Create cria um rascunho com os dados de contato do perfil do proponente (se houver).
func (s *Service) Create(ctx context.Context, actor Actor) (*Project, error) {
	var out db.Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		prof, err := q.GetProponentProfile(ctx, actor.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			prof = db.ProponentProfile{Phones: []byte("[]"), Emails: []byte("[]"), Personal: []byte("{}")}
		}
		out, err = q.CreateProject(ctx, db.CreateProjectParams{CreatedByID: actor.ID, ProponentName: actor.Name, ProponentCpf: actor.CPF,
			ProponentPhones: prof.Phones, ProponentEmails: prof.Emails, ProponentUf: prof.Uf, ProponentMunicipalityIbge: prof.MunicipalityIbge, ProponentJobTitle: prof.JobTitle, ProponentPersonal: prof.Personal})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("project.create", out.ID, map[string]any{"status": out.Status}))
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.q, out, actor)
}

// mutate é o esqueleto de toda gravação de seção: trava o projeto, confere permissão do registro, situação e
// versão, aplica a alteração, registra a auditoria e devolve o projeto já com as pendências recalculadas.
func (s *Service) mutate(ctx context.Context, actor Actor, id uuid.UUID, version int32, upd authz.Decision, section string,
	apply func(q *db.Queries, cur db.Project) (db.Project, error)) (*Project, error) {
	s.settle(ctx, id)
	var saved db.Project
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if !upd.AllowsRecord(refOf(cur).Getter()) {
			return httpapi.ErrForbidden()
		}
		rt, err := s.rightsOf(ctx, q, cur)
		if err != nil {
			return err
		}
		if !rt.editable() {
			return errNotEditable()
		}
		if !rt.canSection(sectionRight(section)) {
			return errNotReturned()
		}
		if cur.Version != version {
			return errVersionConflict(cur.Version)
		}
		if saved, err = apply(q, cur); err != nil {
			return err
		}
		if err := s.markAnswered(ctx, q, id, cur, "section", sectionRight(section)); err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("project.update", id, map[string]any{"section": section, "version": saved.Version}))
	})
	if err != nil {
		return nil, err
	}
	return s.view(ctx, s.q, saved, actor)
}

// SaveProponent grava a seção Proponente. Nome e CPF não mudam (vêm do MT Login).
func (s *Service) SaveProponent(ctx context.Context, actor Actor, id uuid.UUID, version int32, in ProponentSection, upd authz.Decision) (*Project, error) {
	return s.mutate(ctx, actor, id, version, upd, "proponent", func(q *db.Queries, _ db.Project) (db.Project, error) {
		var p problems
		c := normalizeContact(ctx, q, in.Contact, &p)
		repName, repJob := text(in.LegalRepName), text(in.LegalRepJobTitle)
		var repCPF *string
		repPersonal := []byte("{}")
		if in.IsLegalRepresentative != nil && *in.IsLegalRepresentative {
			repName, repJob = "", "" // o próprio proponente é o representante: não há outro a informar
		} else {
			repPersonal, _ = normalizePersonal(ctx, q, in.LegalRepPersonal, "legalRepPersonal", &p)
			if tooLong(repName, maxPersonName) {
				p.add("legalRepName", "Use até 150 caracteres.")
			}
			if tooLong(repJob, maxJobTitle) {
				p.add("legalRepJobTitle", "Use até 100 caracteres.")
			}
			if strings.TrimSpace(in.LegalRepCPF) != "" {
				d, err := cpf.Normalize(in.LegalRepCPF)
				if err != nil {
					p.add("legalRepCpf", "CPF inválido.")
				}
				repCPF = nullText(d)
			}
		}
		if err := p.err(); err != nil {
			return db.Project{}, err
		}
		var isRep pgtype.Bool
		if in.IsLegalRepresentative != nil {
			isRep = pgtype.Bool{Bool: *in.IsLegalRepresentative, Valid: true}
		}
		return q.UpdateProjectProponent(ctx, db.UpdateProjectProponentParams{ID: id, ProponentPhones: c.phones, ProponentEmails: c.emails,
			ProponentUf: c.uf, ProponentMunicipalityIbge: c.ibge, ProponentJobTitle: c.job, IsLegalRepresentative: isRep,
			LegalRepName: repName, LegalRepCpf: repCPF, LegalRepJobTitle: repJob, ProponentPersonal: c.personal, LegalRepPersonal: repPersonal, UpdatedByID: &actor.ID})
	})
}

// SaveInstitution grava a seção Instituição.
func (s *Service) SaveInstitution(ctx context.Context, actor Actor, id uuid.UUID, version int32, in InstitutionSection, upd authz.Decision) (*Project, error) {
	return s.mutate(ctx, actor, id, version, upd, "institution", func(q *db.Queries, cur db.Project) (db.Project, error) {
		var p problems
		arg := db.UpdateProjectInstitutionParams{ID: id, UpdatedByID: &actor.ID}

		switch in.Instrument {
		case "", InstrumentConvenio, InstrumentFomento:
			arg.Instrument = nullText(in.Instrument)
		default:
			p.add("instrument", "Use convênio ou termo de fomento.")
		}
		if strings.TrimSpace(in.CNPJ) != "" {
			d, err := cnpj.Normalize(in.CNPJ)
			if err != nil {
				p.add("cnpj", "CNPJ inválido.")
			}
			arg.InstitutionCnpj = nullText(d)
		}
		arg.InstitutionName = text(in.Name)
		if tooLong(arg.InstitutionName, maxInstitution) {
			p.add("name", "Use até 200 caracteres.")
		}
		s.entityType(ctx, q, cur, in, &arg, &p)
		if arg.Instrument != nil && *arg.Instrument == InstrumentFomento && arg.EntityNature != nil && (*arg.EntityNature != catalog.NaturePrivate || !arg.EntityNonProfit) {
			p.add("instrument", "O Termo de Fomento só pode ser firmado por entidade privada sem fins lucrativos. Para esta entidade, use convênio.")
		}
		arg.InstitutionUf, arg.InstitutionMunicipalityIbge = place(ctx, q, in.UF, in.MunicipalityIBGE, "uf", "municipalityIbge", &p)

		if z := digits(in.Zip); z != "" {
			if len(z) != 8 {
				p.add("zip", "CEP deve ter 8 dígitos.")
			}
			arg.InstitutionZip = z
		}
		arg.InstitutionStreet, arg.InstitutionNumber = text(in.Street), text(in.Number)
		arg.InstitutionComplement, arg.InstitutionDistrict = text(in.Complement), text(in.District)
		for _, c := range []struct {
			v, field string
			max      int
		}{{arg.InstitutionStreet, "street", maxStreet}, {arg.InstitutionNumber, "number", maxNumber}, {arg.InstitutionComplement, "complement", maxComplement}, {arg.InstitutionDistrict, "district", maxDistrict}} {
			if tooLong(c.v, c.max) {
				p.add(c.field, fmt.Sprintf("Use até %d caracteres.", c.max))
			}
		}
		if e := strings.TrimSpace(in.Email); e != "" {
			addr, ok := emailAddress(e)
			if !ok {
				p.add("email", "E-mail inválido.")
			}
			arg.InstitutionEmail = addr
		}
		if ph := strings.TrimSpace(in.Phone); ph != "" {
			n, ok := phoneNumber(ph)
			if !ok {
				p.add("phone", "Telefone inválido. Use DDD e número.")
			}
			arg.InstitutionPhone = n
		}
		switch in.HabilitationStatus {
		case "", HabilitationFull, HabilitationPartial:
			arg.HabilitationStatus = nullText(in.HabilitationStatus)
		default:
			p.add("habilitationStatus", "Use plena ou parcial.")
		}
		arg.HabilitationNumber = text(in.HabilitationNumber)
		if tooLong(arg.HabilitationNumber, maxHabilitation) {
			p.add("habilitationNumber", "Use até 50 caracteres.")
		}
		arg.HabilitationValidUntil = date(in.HabilitationValidUntil, "habilitationValidUntil", &p)

		if err := p.err(); err != nil {
			return db.Project{}, err
		}
		saved, err := q.UpdateProjectInstitution(ctx, arg)
		if err != nil {
			return db.Project{}, err
		}
		// Custos indiretos são do Termo de Fomento: ao passar para Convênio, o valor é zerado.
		if saved.Instrument != nil && *saved.Instrument == InstrumentConvenio && saved.IndirectCostsCents > 0 {
			if err := q.ResetProjectIndirectCosts(ctx, id); err != nil {
				return db.Project{}, err
			}
			saved.IndirectCostsCents = 0
		}
		return saved, nil
	})
}

// entityType resolve o tipo de entidade escolhido e deriva a natureza. Nos tipos comuns a natureza e o "sem fins
// lucrativos" vêm do cadastro; no tipo "Outro" o proponente informa a natureza e o texto livre.
func (s *Service) entityType(ctx context.Context, q *db.Queries, cur db.Project, in InstitutionSection, arg *db.UpdateProjectInstitutionParams, p *problems) {
	if strings.TrimSpace(in.EntityTypeID) == "" {
		return
	}
	id, err := uuid.Parse(in.EntityTypeID)
	if err != nil {
		p.add("entityTypeId", "Tipo de entidade inválido.")
		return
	}
	t, err := q.GetEntityType(ctx, id)
	if err != nil {
		p.add("entityTypeId", "Tipo de entidade não encontrado.")
		return
	}
	// Um tipo desativado depois da escolha continua valendo no projeto que já o usa, mas não pode ser escolhido de novo.
	if !t.Active && (cur.EntityTypeID == nil || *cur.EntityTypeID != id) {
		p.add("entityTypeId", "Este tipo de entidade não está mais disponível.")
		return
	}
	arg.EntityTypeID = &id
	if t.Code != catalog.OtherCode {
		arg.EntityNature, arg.EntityNonProfit = &t.Nature, t.NonProfit
		return
	}
	arg.EntityTypeOther = text(in.EntityTypeOther)
	if tooLong(arg.EntityTypeOther, maxOtherType) {
		p.add("entityTypeOther", "Use até 100 caracteres.")
	}
	switch in.Nature {
	case "":
	case catalog.NaturePublic, catalog.NaturePrivate:
		arg.EntityNature = nullText(in.Nature)
		if in.NonProfit && in.Nature != catalog.NaturePrivate {
			p.add("nonProfit", "Só entidades privadas podem ser sem fins lucrativos.")
		}
		arg.EntityNonProfit = in.NonProfit && in.Nature == catalog.NaturePrivate
	default:
		p.add("nature", "Use pública ou privada.")
	}
}

// SaveProposal grava a seção Proposta, inclusive os períodos e locais de execução (que são regravados por inteiro).
func (s *Service) SaveProposal(ctx context.Context, actor Actor, id uuid.UUID, version int32, in ProposalSection, upd authz.Decision) (*Project, error) {
	return s.mutate(ctx, actor, id, version, upd, "proposal", func(q *db.Queries, _ db.Project) (db.Project, error) {
		var p problems
		arg := db.UpdateProjectProposalParams{ID: id, UpdatedByID: &actor.ID}

		if n := strings.ReplaceAll(in.SigconNumber, " ", ""); n != "" {
			if !sigconRe.MatchString(n) {
				p.add("sigconNumber", "Use o formato NNNN/AAAA, por exemplo 3216/2026.")
			}
			arg.SigconNumber = &n
		}
		arg.SigconRegisteredOn = date(in.SigconRegisteredOn, "sigconRegisteredOn", &p)
		if arg.SigconRegisteredOn.Valid && arg.SigconRegisteredOn.Time.After(s.today()) {
			p.add("sigconRegisteredOn", "A data de cadastro no SIGCon não pode ser futura.")
		}
		arg.Title = text(in.Title)
		arg.ObjectDescription, arg.Justification = multiline(in.Object), multiline(in.Justification)
		for _, c := range []struct {
			v, field string
			max      int
		}{{arg.Title, "title", maxTitle}, {arg.ObjectDescription, "object", maxObject}, {arg.Justification, "justification", maxJustify}} {
			if tooLong(c.v, c.max) {
				p.add(c.field, fmt.Sprintf("Use até %d caracteres.", c.max))
			}
		}
		switch in.ObjectNature {
		case "", "works", "goods", "services", "event", "other":
			arg.ObjectNature = nullText(in.ObjectNature)
		default:
			p.add("objectNature", "Natureza do objeto inválida.")
		}
		arg.HasArtisticHiring, arg.Calamity = in.HasArtisticHiring, in.Calamity

		periods, err := executionPeriodParams(ctx, q, id, in.ExecutionPeriods, &p)
		if err != nil {
			return db.Project{}, err
		}
		if err := p.err(); err != nil {
			return db.Project{}, err
		}
		if err := q.ClearProjectExecutionPeriods(ctx, id); err != nil {
			return db.Project{}, err
		}
		for _, row := range periods {
			if err := q.AddProjectExecutionPeriod(ctx, row); err != nil {
				return db.Project{}, err
			}
		}
		saved, err := q.UpdateProjectProposal(ctx, arg)
		if err != nil {
			return db.Project{}, err
		}
		// Os cachês só existem se o projeto prevê contratação de artistas: desmarcar zera os valores.
		if !saved.HasArtisticHiring && (saved.ArtistFeesCents > 0 || saved.RegionalArtistFeesCents > 0 || saved.GovernorWaiver) {
			if err := q.ResetProjectArtistFunding(ctx, id); err != nil {
				return db.Project{}, err
			}
			saved.ArtistFeesCents, saved.RegionalArtistFeesCents, saved.GovernorWaiver = 0, 0, false
		}
		return saved, nil
	})
}

// Delete exclui um rascunho. del é a decisão da ação delete.
func (s *Service) Delete(ctx context.Context, actor Actor, id uuid.UUID, del authz.Decision) error {
	var keys []string
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		cur, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if !del.AllowsRecord(refOf(cur).Getter()) {
			return httpapi.ErrForbidden()
		}
		if cur.Status != StatusDraft {
			return errNotEditable()
		}
		var err2 error
		if keys, err2 = q.ListProjectFileKeys(ctx, id); err2 != nil {
			return err2
		}
		if _, err := q.DeleteProject(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("project.delete", id, map[string]any{"status": cur.Status}))
	})
	if err == nil {
		s.deleteObjects(ctx, keys)
	}
	return err
}

func unmarshalJSON(b []byte, dst any) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, dst)
}

// maskCPF oculta os dígitos do CPF (***.456.789-**); valores que não são um CPF de 11 dígitos saem vazios.
func maskCPF(d string) string {
	if d == "" {
		return ""
	}
	return cpf.Mask(d)
}
