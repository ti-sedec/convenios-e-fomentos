package catalog

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// Naturezas de entidade e casas legislativas.
const (
	NaturePublic  = "public"
	NaturePrivate = "private"

	ChamberState   = "state"
	ChamberFederal = "federal"

	// OtherCode é o tipo "Outro": o cadastro do projeto pede o texto livre e a natureza.
	OtherCode = "other"
)

// Actor é o administrador que executa a operação, para a auditoria.
type Actor struct {
	ID uuid.UUID
	IP string
}

func (a Actor) entry(action, entity string, id uuid.UUID, before, after any) audit.Entry {
	payload := map[string]any{}
	if before != nil {
		payload["before"] = before
	}
	if after != nil {
		payload["after"] = after
	}
	return audit.Entry{ActorType: "admin", ActorID: a.ID.String(), Action: action, Entity: entity, EntityID: id.String(), Payload: payload, IP: a.IP}
}

// Service implementa as operações dos catálogos.
type Service struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	audit audit.Recorder
}

// NewService cria o serviço.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, q: db.New(pool)} }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpapi.ErrNotFound()
	}
	return err
}

// cleanName normaliza espaços e valida o tamanho.
func cleanName(name string, max int) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len([]rune(name)) > max {
		return "", fieldError("name", "Informe um nome de até "+strconv.Itoa(max)+" caracteres.")
	}
	return name, nil
}

// ---- Tipos de entidade ----

// EntityType é um tipo de entidade proponente.
type EntityType struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Nature    string    `json:"nature"`
	NonProfit bool      `json:"nonProfit"`
	Active    bool      `json:"active"`
	SortOrder int       `json:"sortOrder"`
	// IsOther indica o tipo "Outro": o cadastro pede o texto livre e a natureza (pública ou privada).
	IsOther   bool      `json:"isOther"`
	IsSystem  bool      `json:"isSystem"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// EntityTypeInput é o corpo de criação e alteração. Code só vale na criação.
type EntityTypeInput struct {
	Code      string `json:"code" validate:"omitempty"`
	Name      string `json:"name" validate:"required"`
	Nature    string `json:"nature" validate:"required,oneof=public private"`
	NonProfit bool   `json:"nonProfit"`
	Active    *bool  `json:"active"`
	SortOrder int    `json:"sortOrder" validate:"min=0,max=9999"`
}

var codeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func entityTypeView(e db.EntityType) EntityType {
	return EntityType{ID: e.ID, Code: e.Code, Name: e.Name, Nature: e.Nature, NonProfit: e.NonProfit, Active: e.Active, SortOrder: int(e.SortOrder),
		IsOther: e.Code == OtherCode, IsSystem: e.IsSystem, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func errEntityTypeTaken() error {
	return httpapi.ErrConflict("entity_type_taken", "Já existe um tipo de entidade com este código ou nome.")
}

func validateEntityType(in EntityTypeInput) (string, error) {
	name, err := cleanName(in.Name, 100)
	if err != nil {
		return "", err
	}
	if in.NonProfit && in.Nature != NaturePrivate {
		return "", fieldError("nonProfit", "Só entidades privadas podem ser sem fins lucrativos.")
	}
	return name, nil
}

// ListEntityTypes devolve os tipos, opcionalmente só os ativos.
func (s *Service) ListEntityTypes(ctx context.Context, onlyActive bool) ([]EntityType, error) {
	rows, err := s.q.ListEntityTypes(ctx, pgtype.Bool{Bool: true, Valid: onlyActive})
	if err != nil {
		return nil, err
	}
	out := make([]EntityType, len(rows))
	for i, r := range rows {
		out[i] = entityTypeView(r)
	}
	return out, nil
}

// GetEntityType devolve um tipo.
func (s *Service) GetEntityType(ctx context.Context, id uuid.UUID) (*EntityType, error) {
	r, err := s.q.GetEntityType(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	v := entityTypeView(r)
	return &v, nil
}

// CreateEntityType cria um tipo de entidade.
func (s *Service) CreateEntityType(ctx context.Context, actor Actor, in EntityTypeInput) (*EntityType, error) {
	name, err := validateEntityType(in)
	if err != nil {
		return nil, err
	}
	if !codeRe.MatchString(in.Code) || in.Code == OtherCode {
		return nil, fieldError("code", "Use de 2 a 40 caracteres: letras minúsculas, números e _, começando por letra.")
	}
	active := in.Active == nil || *in.Active
	var out EntityType
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.CreateEntityType(ctx, db.CreateEntityTypeParams{Code: in.Code, Name: name, Nature: in.Nature, NonProfit: in.NonProfit, Active: active, SortOrder: int32(in.SortOrder)}) //nolint:gosec // validado: 0 a 9999
		if database.IsUniqueViolation(err) {
			return errEntityTypeTaken()
		}
		if err != nil {
			return err
		}
		out = entityTypeView(row)
		return s.audit.Record(ctx, q, actor.entry("entity-type.create", "entity_type", row.ID, nil, out))
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateEntityType altera um tipo. O código não muda. O tipo de sistema ("Outro") não pode ser desativado.
func (s *Service) UpdateEntityType(ctx context.Context, actor Actor, id uuid.UUID, in EntityTypeInput) (*EntityType, error) {
	name, err := validateEntityType(in)
	if err != nil {
		return nil, err
	}
	var out EntityType
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetEntityTypeForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if in.Code != "" && in.Code != before.Code {
			return fieldError("code", "O código de um tipo não pode ser alterado.")
		}
		active := before.Active
		if in.Active != nil {
			active = *in.Active
		}
		if before.IsSystem && !active {
			return httpapi.ErrConflict("entity_type_system", "Este tipo é do sistema e não pode ser desativado.")
		}
		row, err := q.UpdateEntityType(ctx, db.UpdateEntityTypeParams{ID: id, Name: name, Nature: in.Nature, NonProfit: in.NonProfit, Active: active, SortOrder: int32(in.SortOrder)}) //nolint:gosec // validado: 0 a 9999
		if database.IsUniqueViolation(err) {
			return errEntityTypeTaken()
		}
		if err != nil {
			return err
		}
		out = entityTypeView(row)
		return s.audit.Record(ctx, q, actor.entry("entity-type.update", "entity_type", id, entityTypeView(before), out))
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteEntityType exclui um tipo que não é do sistema e não está em uso.
func (s *Service) DeleteEntityType(ctx context.Context, actor Actor, id uuid.UUID) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetEntityTypeForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if before.IsSystem {
			return httpapi.ErrConflict("entity_type_system", "Este tipo é do sistema e não pode ser excluído.")
		}
		if _, err := q.DeleteEntityType(ctx, id); err != nil {
			if database.IsForeignKeyViolation(err) {
				return httpapi.ErrConflict("entity_type_in_use", "O tipo está em uso por projetos. Desative-o em vez de excluir.")
			}
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("entity-type.delete", "entity_type", id, entityTypeView(before), nil))
	})
}

type defaultEntityType struct {
	code, name, nature string
	nonProfit, system  bool
}

// defaultEntityTypes é a lista inicial. Os tipos privados sem fins lucrativos seguem o "Status Jurídico" do SIGCon.
var defaultEntityTypes = []defaultEntityType{
	{"municipal_government", "Prefeitura Municipal", NaturePublic, false, false},
	{"public_consortium", "Consórcio Público / Intermunicipal", NaturePublic, false, false},
	{"municipal_fund", "Fundo Municipal", NaturePublic, false, false},
	{"state_body", "Órgão ou entidade da administração estadual", NaturePublic, false, false},
	{"federal_body", "Órgão ou entidade da administração federal", NaturePublic, false, false},
	{"public_company_foundation", "Empresa Pública / Fundação Pública", NaturePublic, false, false},
	{"association", "Associação", NaturePrivate, true, false},
	{"union_federation", "Sindicato / Federação", NaturePrivate, true, false},
	{"private_foundation", "Fundação Privada", NaturePrivate, true, false},
	{"cooperative", "Cooperativa", NaturePrivate, true, false},
	{"religious", "Organização Religiosa", NaturePrivate, true, false},
	{"philanthropic", "Entidade Filantrópica (APAE, Pestalozzi)", NaturePrivate, true, false},
	{"oscip", "OSCIP", NaturePrivate, true, false},
	{"ngo", "ONG", NaturePrivate, true, false},
	{"health_org", "Organização de Saúde", NaturePrivate, true, false},
	{"international_org", "Organismo Internacional", NaturePrivate, true, false},
	{"public_service_concessionaire", "Concessionária de Serviço Público", NaturePrivate, false, false},
	{OtherCode, "Outro", NaturePrivate, false, true},
}

// EnsureDefaults carrega os padrões que ainda não existem: tipos de entidade e municípios. Não
// altera o que um administrador já editou.
func (s *Service) EnsureDefaults(ctx context.Context) error {
	for i, d := range defaultEntityTypes {
		order := int32((i + 1) * 10)
		if d.code == OtherCode {
			order = 9999 // sempre por último
		}
		if _, err := s.q.InsertDefaultEntityType(ctx, db.InsertDefaultEntityTypeParams{Code: d.code, Name: d.name, Nature: d.nature, NonProfit: d.nonProfit, IsSystem: d.system, SortOrder: order}); err != nil {
			return err
		}
	}
	return s.ensureMunicipalities(ctx)
}

// ---- Parlamentares ----

// Parliamentarian é um parlamentar que destina emendas.
type Parliamentarian struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Chamber string    `json:"chamber"`
	Active  bool      `json:"active"`
	// InUse indica que alguma emenda de projeto cita o parlamentar; ele não pode ser excluído, só desativado.
	// Só é calculado nas respostas da API; omitido quando falso, o que também o mantém fora do payload da auditoria.
	InUse     bool      `json:"inUse,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ParliamentarianInput é o corpo de criação e alteração.
type ParliamentarianInput struct {
	Name    string `json:"name" validate:"required"`
	Chamber string `json:"chamber" validate:"required,oneof=state federal"`
	Active  *bool  `json:"active"`
}

func parliamentarianView(p db.Parliamentarian) Parliamentarian {
	return Parliamentarian{ID: p.ID, Name: p.Name, Chamber: p.Chamber, Active: p.Active, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

func errParliamentarianTaken() error {
	return httpapi.ErrConflict("parliamentarian_taken", "Já existe um parlamentar com este nome nesta casa.")
}

// ParliamentarianFilter filtra a listagem.
type ParliamentarianFilter struct {
	Chamber    string
	OnlyActive bool
	Search     string
}

// ListParliamentarians devolve os parlamentares por nome.
func (s *Service) ListParliamentarians(ctx context.Context, f ParliamentarianFilter) ([]Parliamentarian, error) {
	p := db.ListParliamentariansParams{Active: pgtype.Bool{Bool: true, Valid: f.OnlyActive}}
	if f.Chamber != "" {
		if f.Chamber != ChamberState && f.Chamber != ChamberFederal {
			return nil, fieldError("chamber", "Use state ou federal.")
		}
		p.Chamber = &f.Chamber
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		if len([]rune(q)) > 60 {
			return nil, fieldError("q", "Use até 60 caracteres.")
		}
		p.Search = &q
	}
	rows, err := s.q.ListParliamentarians(ctx, p)
	if err != nil {
		return nil, err
	}
	ids, err := s.q.ListParliamentariansInUse(ctx)
	if err != nil {
		return nil, err
	}
	inUse := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		inUse[id] = true
	}
	out := make([]Parliamentarian, len(rows))
	for i, r := range rows {
		out[i] = parliamentarianView(r)
		out[i].InUse = inUse[r.ID]
	}
	return out, nil
}

// GetParliamentarian devolve um parlamentar.
func (s *Service) GetParliamentarian(ctx context.Context, id uuid.UUID) (*Parliamentarian, error) {
	r, err := s.q.GetParliamentarian(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	v := parliamentarianView(r)
	if v.InUse, err = s.q.ParliamentarianInUse(ctx, &id); err != nil {
		return nil, err
	}
	return &v, nil
}

// CreateParliamentarian cria um parlamentar.
func (s *Service) CreateParliamentarian(ctx context.Context, actor Actor, in ParliamentarianInput) (*Parliamentarian, error) {
	name, err := cleanName(in.Name, 120)
	if err != nil {
		return nil, err
	}
	active := in.Active == nil || *in.Active
	var out Parliamentarian
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		row, err := q.CreateParliamentarian(ctx, db.CreateParliamentarianParams{Name: name, Chamber: in.Chamber, Active: active})
		if database.IsUniqueViolation(err) {
			return errParliamentarianTaken()
		}
		if err != nil {
			return err
		}
		out = parliamentarianView(row)
		return s.audit.Record(ctx, q, actor.entry("parliamentarian.create", "parliamentarian", row.ID, nil, out))
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateParliamentarian altera um parlamentar.
func (s *Service) UpdateParliamentarian(ctx context.Context, actor Actor, id uuid.UUID, in ParliamentarianInput) (*Parliamentarian, error) {
	name, err := cleanName(in.Name, 120)
	if err != nil {
		return nil, err
	}
	var out Parliamentarian
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetParliamentarianForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		active := before.Active
		if in.Active != nil {
			active = *in.Active
		}
		row, err := q.UpdateParliamentarian(ctx, db.UpdateParliamentarianParams{ID: id, Name: name, Chamber: in.Chamber, Active: active})
		if database.IsUniqueViolation(err) {
			return errParliamentarianTaken()
		}
		if err != nil {
			return err
		}
		out = parliamentarianView(row)
		return s.audit.Record(ctx, q, actor.entry("parliamentarian.update", "parliamentarian", id, parliamentarianView(before), out))
	})
	if err != nil {
		return nil, err
	}
	if out.InUse, err = s.q.ParliamentarianInUse(ctx, &id); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteParliamentarian exclui um parlamentar que não está em uso.
func (s *Service) DeleteParliamentarian(ctx context.Context, actor Actor, id uuid.UUID) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetParliamentarianForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if _, err := q.DeleteParliamentarian(ctx, id); err != nil {
			if database.IsForeignKeyViolation(err) {
				return httpapi.ErrConflict("parliamentarian_in_use", "O parlamentar está em uso por projetos. Desative-o em vez de excluir.")
			}
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("parliamentarian.delete", "parliamentarian", id, parliamentarianView(before), nil))
	})
}
