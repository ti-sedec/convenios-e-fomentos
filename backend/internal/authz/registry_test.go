package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistroDeAcoes(t *testing.T) {
	ok := Action{ID: "admin::content.artigo.create", Domain: DomainAdmin, Category: CategoryCollectionTypes, Name: "Criar", Resource: "artigo"}
	tests := []struct {
		nome   string
		mutate func(*Action)
		erro   bool
	}{
		{"válida", func(*Action) {}, false},
		{"id sem domínio", func(a *Action) { a.ID = "content.artigo.create" }, true},
		{"id com maiúscula no recurso", func(a *Action) { a.ID = "admin::Content.artigo.create" }, true},
		{"domínio diverge do prefixo", func(a *Action) { a.Domain = "api" }, true},
		{"prefixo api não existe mais", func(a *Action) { a.ID = "api::artigo.find"; a.Domain = "api" }, true},
		{"sem nome", func(a *Action) { a.Name = "" }, true},
		{"sem categoria", func(a *Action) { a.Category = "" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			a := ok
			tt.mutate(&a)
			err := NewRegistry().Register(a)
			if tt.erro {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	r := NewRegistry()
	require.NoError(t, r.Register(ok))
	assert.Error(t, r.Register(ok), "duplicada")
	assert.Panics(t, func() { r.MustRegister(ok) })
}

func TestAgrupamentoPorCategoria(t *testing.T) {
	r := NewRegistry()
	RegisterCoreActions(r)
	r.MustRegister(Action{ID: "admin::content.document.read", Domain: DomainAdmin, Category: CategoryCollectionTypes, Name: "Ler", Resource: "document"})

	groups := r.ByCategory(DomainAdmin)
	require.GreaterOrEqual(t, len(groups), 2)
	assert.Equal(t, CategoryCollectionTypes, groups[0].Category, "ordem estável das categorias")
	assert.Equal(t, CategorySettings, groups[1].Category)
	for _, g := range groups {
		for _, a := range g.Actions {
			assert.Equal(t, DomainAdmin, a.Domain)
		}
	}
	assert.Contains(t, r.Resources(DomainAdmin), "document")
	_, found := r.Get("admin::settings.users.create")
	assert.True(t, found)
	assert.Len(t, r.All(), len(r.IDs(DomainAdmin)))
}

func TestRegistroDeCondicoes(t *testing.T) {
	r := NewConditionRegistry()
	RegisterStandardConditions(r, fakeMembers{})
	ids := []string{}
	for _, c := range r.All() {
		ids = append(ids, c.ID)
		assert.NotEmpty(t, c.Name)
	}
	assert.Equal(t, []string{"admin::is-creator", "admin::has-same-role-as-creator", "admin::same-department"}, ids)

	h := func(context.Context, Subject) (Result, error) { return Allow(), nil }
	assert.Error(t, r.Register(Condition{ID: "admin::is-creator", Name: "x", Handler: h}), "duplicada")
	assert.Error(t, r.Register(Condition{ID: "x", Handler: h}), "sem nome")
	assert.Error(t, r.Register(Condition{ID: "y", Name: "y"}), "sem handler")
	assert.Panics(t, func() { r.MustRegister(Condition{ID: "admin::is-creator", Name: "x", Handler: h}) })
	assert.NoError(t, r.Register(Condition{ID: "admin::custom", Name: "Custom", Handler: h}))
}

func TestSubject(t *testing.T) {
	a, b := role("a"), role(SuperAdminRoleCode)
	s := Subject{Type: SubjectAdmin, Roles: []Role{a, b}}
	assert.True(t, s.IsSuperAdmin())
	assert.Equal(t, []Role{a, b}, s.Roles)
	assert.Len(t, s.RoleIDs(), 2)
	assert.False(t, Subject{Type: SubjectAdmin, Roles: []Role{a}}.IsSuperAdmin())
}
