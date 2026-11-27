package authz

import (
	"context"

	"github.com/google/uuid"
)

// SuperAdminRoleCode identifica a role Super Admin (curinga no motor).
const SuperAdminRoleCode = "super-admin"

// ProponentRoleCode identifica a role de proponente, atribuída a quem entra pelo MT Login pela primeira vez.
const ProponentRoleCode = "proponent"

// SubjectType é o tipo de sujeito autenticado.
type SubjectType string

// Tipos de sujeito.
const (
	SubjectAdmin SubjectType = "admin"
)

// Role referencia uma role do sujeito.
type Role struct {
	ID   uuid.UUID
	Code string
}

// Subject é quem executa a ação, já autenticado.
type Subject struct {
	Type         SubjectType
	ID           uuid.UUID
	Roles        []Role
	DepartmentID *uuid.UUID
}

// IsSuperAdmin informa se o sujeito é administrador com a role Super Admin.
func (s Subject) IsSuperAdmin() bool {
	if s.Type != SubjectAdmin {
		return false
	}
	for _, r := range s.Roles {
		if r.Code == SuperAdminRoleCode {
			return true
		}
	}
	return false
}

// RoleIDs devolve os IDs das roles.
func (s Subject) RoleIDs() []uuid.UUID {
	ids := make([]uuid.UUID, len(s.Roles))
	for i, r := range s.Roles {
		ids[i] = r.ID
	}
	return ids
}

type subjectKey struct{}

// WithSubject guarda o sujeito autenticado no contexto da requisição.
func WithSubject(ctx context.Context, s Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, s)
}

// SubjectFrom devolve o sujeito guardado por WithSubject.
func SubjectFrom(ctx context.Context) (Subject, bool) {
	s, ok := ctx.Value(subjectKey{}).(Subject)
	return s, ok
}
