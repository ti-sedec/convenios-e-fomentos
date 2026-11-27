package app_test

import (
	"time"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
)

// fixtureDoc é o recurso de exemplo dos testes de RBAC: nenhum recurso real do sistema oferece permissão por campo, então os
// testes registram este (veja app.WithActions). As tags alimentam o authz.Schema: "json" é o nome lógico do campo e
// authz:"system" marca o campo de sistema (sempre legível, nunca gravável).
type fixtureDoc struct {
	ID                uuid.UUID  `json:"id" db:"id" authz:"system"`
	Titulo            string     `json:"titulo" db:"titulo"`
	Conteudo          string     `json:"conteudo" db:"conteudo"`
	Status            string     `json:"status" db:"status"`
	ObservacaoInterna string     `json:"observacao_interna" db:"observacao_interna"`
	DepartmentID      *uuid.UUID `json:"department_id" db:"department_id"`
	PublishedAt       *time.Time `json:"published_at" db:"published_at" authz:"system"`
	CreatedByID       *uuid.UUID `json:"created_by_id" db:"created_by_id" authz:"system"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at" authz:"system"`
}

const fixtureResource = "document"

func registerFixtureResource(reg *authz.Registry) {
	schema, err := authz.NewSchema(fixtureResource, fixtureDoc{})
	if err != nil {
		panic(err)
	}
	act := func(id, name string, fields, conds bool) authz.Action {
		return authz.Action{ID: id, Domain: authz.DomainAdmin, Category: authz.CategoryCollectionTypes, Name: name, Resource: fixtureResource,
			SupportsFields: fields, SupportsConditions: conds, SupportsLocales: true}
	}
	reg.MustRegister(
		act("admin::content.document.create", "Criar documentos", true, false),
		act("admin::content.document.read", "Ler documentos", true, true),
		act("admin::content.document.update", "Atualizar documentos", true, true),
		act("admin::content.document.delete", "Excluir documentos", false, true),
		act("admin::content.document.publish", "Publicar documentos", false, true),
	)
	reg.RegisterSchema(fixtureResource, schema)
}
