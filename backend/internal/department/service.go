// Pacote department administra os departamentos, que delimitam o alcance da condição
// `admin::same-department` e agrupam administradores e documentos.
package department

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// View é o departamento exposto pela API, com o que está vinculado a ele.
type View struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// IsFinalistic marca a área finalística: só departamento marcado pode ser indicado para enquadrar um projeto.
	IsFinalistic bool  `json:"isFinalistic"`
	UserCount    int64 `json:"userCount"`
	// ReviewerCount são os Revisores ativos do departamento (sem nenhum, ele não pode ser indicado para enquadrar projetos).
	ReviewerCount int64 `json:"reviewerCount"`
	// ProjectCount são os projetos que o referenciam (enquadramento pendente ou confirmado).
	ProjectCount int64 `json:"projectCount"`
}

// Actor é o administrador que executa a operação, para a auditoria.
type Actor struct {
	ID uuid.UUID
	IP string
}

func (a Actor) entry(action string, id uuid.UUID, before, after any) audit.Entry {
	payload := map[string]any{}
	if before != nil {
		payload["before"] = before
	}
	if after != nil {
		payload["after"] = after
	}
	return audit.Entry{ActorType: "admin", ActorID: a.ID.String(), Action: action, Entity: "department", EntityID: id.String(), Payload: payload, IP: a.IP}
}

func errNameTaken() error {
	return httpapi.ErrConflict("department_name_taken", "Já existe um departamento com este nome.")
}

func errInUse(users, projects int64) error {
	e := httpapi.ErrConflict("department_in_use", "O departamento tem administradores ou projetos vinculados e não pode ser excluído. Mude a lotação dos administradores e conclua os enquadramentos antes.")
	e.Details = map[string]any{"userCount": users, "projectCount": projects}
	return e
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpapi.ErrNotFound()
	}
	return err
}

// cleanName normaliza e valida o nome.
func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || len([]rune(name)) > 100 {
		return "", httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "name", Message: "Informe um nome de até 100 caracteres."}}})
	}
	return name, nil
}

// Service implementa as operações sobre departamentos.
type Service struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	audit audit.Recorder
}

// NewService cria o serviço.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool, q: db.New(pool)} }

// Input são os dados editáveis de um departamento. IsFinalistic nulo significa "não informado": na criação vale falso e na
// atualização o valor atual é mantido (um cliente que só conhece o nome não desmarca a área sem querer).
type Input struct {
	Name         string
	IsFinalistic *bool
}

// List devolve os departamentos por nome; onlyFinalistic deixa só as áreas finalísticas.
func (s *Service) List(ctx context.Context, onlyFinalistic bool) ([]View, error) {
	rows, err := s.q.ListDepartmentsWithCounts(ctx, onlyFinalistic)
	if err != nil {
		return nil, err
	}
	out := make([]View, len(rows))
	for i, r := range rows {
		out[i] = View{ID: r.ID, Name: r.Name, IsFinalistic: r.IsFinalistic, UserCount: r.UserCount, ReviewerCount: r.ReviewerCount, ProjectCount: r.ProjectCount}
	}
	return out, nil
}

// Get devolve um departamento.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*View, error) {
	return s.view(ctx, s.q, id)
}

func (s *Service) view(ctx context.Context, q *db.Queries, id uuid.UUID) (*View, error) {
	r, err := q.GetDepartmentWithCounts(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	return &View{ID: r.ID, Name: r.Name, IsFinalistic: r.IsFinalistic, UserCount: r.UserCount, ReviewerCount: r.ReviewerCount, ProjectCount: r.ProjectCount}, nil
}

// Create cria um departamento.
func (s *Service) Create(ctx context.Context, actor Actor, in Input) (*View, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, err
	}
	var out *View
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		d, err := q.CreateDepartment(ctx, db.CreateDepartmentParams{Name: name, IsFinalistic: in.IsFinalistic != nil && *in.IsFinalistic})
		if database.IsUniqueViolation(err) {
			return errNameTaken()
		}
		if err != nil {
			return err
		}
		if err := s.audit.Record(ctx, q, actor.entry("department.create", d.ID, nil, map[string]any{"name": d.Name, "isFinalistic": d.IsFinalistic})); err != nil {
			return err
		}
		out, err = s.view(ctx, q, d.ID)
		return err
	})
	return out, err
}

// Update renomeia o departamento e/ou muda o marcador de área finalística.
func (s *Service) Update(ctx context.Context, actor Actor, id uuid.UUID, in Input) (*View, error) {
	name, err := cleanName(in.Name)
	if err != nil {
		return nil, err
	}
	var out *View
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetDepartmentForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		finalistic := before.IsFinalistic
		if in.IsFinalistic != nil {
			finalistic = *in.IsFinalistic
		}
		after, err := q.UpdateDepartment(ctx, db.UpdateDepartmentParams{ID: id, Name: name, IsFinalistic: finalistic})
		if database.IsUniqueViolation(err) {
			return errNameTaken()
		}
		if err != nil {
			return err
		}
		if err := s.audit.Record(ctx, q, actor.entry("department.update", id, map[string]any{"name": before.Name, "isFinalistic": before.IsFinalistic}, map[string]any{"name": after.Name, "isFinalistic": after.IsFinalistic})); err != nil {
			return err
		}
		out, err = s.view(ctx, q, id)
		return err
	})
	return out, err
}

// Delete exclui o departamento se nenhum administrador ou documento depender dele. Sem essa
// checagem a exclusão deixaria os vinculados sem departamento e mudaria em silêncio o alcance
// de quem tem permissões `admin::same-department`.
func (s *Service) Delete(ctx context.Context, actor Actor, id uuid.UUID) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		d, err := q.GetDepartmentForUpdate(ctx, id) // a trava impede novos vínculos durante a exclusão
		if err != nil {
			return notFound(err)
		}
		v, err := s.view(ctx, q, id)
		if err != nil {
			return err
		}
		if v.UserCount > 0 || v.ProjectCount > 0 {
			return errInUse(v.UserCount, v.ProjectCount)
		}
		if _, err := q.DeleteDepartment(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("department.delete", id, map[string]any{"name": d.Name}, nil))
	})
}
