// Pacote seed cria os dados iniciais do sistema. É idempotente e não destrutivo: rodar de
// novo não duplica nada nem desfaz alterações feitas depois pelos administradores.
//
//   - Roles de admin faltantes são criadas, com as permissões padrão aplicadas somente na criação.
//   - Os CPFs de SuperAdminCPFs viram Super Admin (login pelo MT Login), em qualquer ambiente.
//   - Em desenvolvimento, cria um administrador de exemplo por role, com CPFs fictícios (DevUsers):
//     servem para ver as listagens e para os testes, mas não conseguem entrar pelo MT Login.
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wagner/convenios-e-fomentos/backend/internal/admin"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/catalog"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/project"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
)

// Deps são as dependências do seed.
type Deps struct {
	Pool        *pgxpool.Pool
	Engine      *authz.Engine
	Log         *slog.Logger
	Development bool
	// SuperAdminCPFs são os CPFs que recebem a role Super Admin (ver SEED_SUPER_ADMIN_CPF).
	SuperAdminCPFs []string
}

// Códigos das roles de administrador criadas pelo seed.
const (
	RoleSuperAdmin = authz.SuperAdminRoleCode
	RoleReviewer   = "department-reviewer"
	RoleProponent  = authz.ProponentRoleCode
	// RoleSupervisor também é criado pela migração 00022.
	RoleSupervisor = "administrative-supervisor"
	// RoleAnalyst também é criado pela migração 00023.
	RoleAnalyst = "analyst"
	// RoleSecretary é criado pela migração 00024.
	RoleSecretary = "state-secretary"
)

// Ações de departamentos que o Analista recebe (ele os administra, menos criar).
const (
	actionDepartmentsRead   = "admin::settings.departments.read"
	actionDepartmentsUpdate = "admin::settings.departments.update"
	actionDepartmentsDelete = "admin::settings.departments.delete"
)

// staffRead é a leitura de projeto da equipe: só projeto enviado ou arquivado, nunca rascunho.
var staffRead = authz.Permission{Action: project.ActionRead, Conditions: []string{project.ConditionSubmitted}}

// staffReference são as leituras de catálogo e de parâmetros que todos os perfis de equipe têm.
func staffReference() []authz.Permission {
	return []authz.Permission{
		{Action: catalog.ActionEntityTypesRead}, {Action: catalog.ActionMunicipalitiesRead},
		{Action: catalog.ActionParliamentariansRead}, {Action: rules.ActionRead},
	}
}

// Departamentos de exemplo.
// Departments são os departamentos de exemplo. Em desenvolvimento nascem como área finalística; em produção nascem sem a marca,
// porque quem administra decide quais departamentos recebem projetos para enquadramento. O marcador só vale na criação: um novo
// seed nunca altera a escolha feita depois.
var Departments = []string{"Financeiro", "Jurídico"}

type roleSpec struct {
	code, name, description string
	system                  bool
	perms                   []authz.Permission
	// topUp acrescenta as permissões padrão que ainda faltam numa role que já existe (sem alterar nem remover
	// nenhuma). Serve às roles de sistema cujo conjunto cresce a cada fase.
	topUp bool
}

func adminRoles() []roleSpec {
	return []roleSpec{
		{code: RoleSuperAdmin, name: "Super Admin", description: "Acesso total ao sistema.", system: true},
		{
			code: RoleProponent, name: "Proponente", description: "Quem entra pelo MT Login e cadastra projetos de convênio ou fomento. Recebe esta role automaticamente no primeiro acesso.", system: true, topUp: true,
			perms: []authz.Permission{
				{Action: catalog.ActionEntityTypesRead}, {Action: catalog.ActionMunicipalitiesRead},
				{Action: catalog.ActionParliamentariansRead}, {Action: rules.ActionRead},
				{Action: project.ActionProfileRead}, {Action: project.ActionProfileUpdate},
				{Action: project.ActionCreate},
				{Action: project.ActionRead, Conditions: []string{"admin::is-creator", project.ConditionMember}},
				{Action: project.ActionUpdate, Conditions: []string{"admin::is-creator", project.ConditionEditor}},
				{Action: project.ActionDelete, Conditions: []string{"admin::is-creator"}},
				{Action: project.ActionSubmit, Conditions: []string{"admin::is-creator", project.ConditionEditor}},
				{Action: project.ActionWithdraw, Conditions: []string{"admin::is-creator"}},
				{Action: project.ActionShare, Conditions: []string{"admin::is-creator"}},
				{Action: project.ActionReport, Conditions: []string{"admin::is-creator", project.ConditionMember}},
			},
		},
		{
			code: RoleReviewer, name: "Revisor do Departamento", description: "Vê os projetos enviados do próprio departamento e confirma ou recusa o enquadramento.",
			perms: append([]authz.Permission{
				{Action: project.ActionRead, Conditions: []string{project.ConditionSubmittedSameDepartment}},
				{Action: project.ActionReviewFraming, Conditions: []string{project.ConditionSubmittedSameDepartment}},
				{Action: project.ActionReport, Conditions: []string{project.ConditionSubmittedSameDepartment}},
			}, staffReference()...),
		},
		{
			code: RoleAnalyst, name: "Analista", system: true,
			description: "Assume a análise dos projetos enviados, confere documentos e informações, devolve para complementação, arquiva por falta de documentos obrigatórios e administra os departamentos.",
			perms: append([]authz.Permission{
				staffRead,
				{Action: project.ActionRequestComplement, Conditions: []string{project.ConditionSubmitted}},
				{Action: project.ActionArchive, Conditions: []string{project.ConditionSubmitted}},
				{Action: project.ActionStartAnalysis, Conditions: []string{project.ConditionSubmitted}},
				{Action: project.ActionReviewItem, Conditions: []string{project.ConditionSubmitted}},
				{Action: project.ActionAssignDepartment, Conditions: []string{project.ConditionSubmitted}},
				{Action: project.ActionReport, Conditions: []string{project.ConditionSubmitted}},
				{Action: actionDepartmentsRead}, {Action: actionDepartmentsUpdate}, {Action: actionDepartmentsDelete},
			}, staffReference()...),
		},
		{
			code: RoleSupervisor, name: "Supervisor Administrador", system: true,
			description: "Acompanha todos os projetos enviados, somente leitura. Recebe e-mails sobre documentação regular, enquadramento recusado e arquivamento.",
			perms:       append([]authz.Permission{staffRead, {Action: project.ActionReport, Conditions: []string{project.ConditionSubmitted}}}, staffReference()...),
		},
		{
			code: RoleSecretary, name: "Secretário de Estado", system: true,
			description: "Vê todos os projetos enviados e pode negá-los a qualquer momento, arquivando-os na hora.",
			perms:       append([]authz.Permission{staffRead, {Action: project.ActionDeny, Conditions: []string{project.ConditionSubmitted}}, {Action: project.ActionReport, Conditions: []string{project.ConditionSubmitted}}}, staffReference()...),
		},
	}
}

// DefaultPermissions devolve as permissões padrão do perfil de sistema com esse código (nil se não existe). Os perfis de equipe
// nascem nas migrações e o seed só declara os mesmos valores, então os testes usam esta função para conferir que os dois batem.
func DefaultPermissions(code string) []authz.Permission {
	for _, spec := range adminRoles() {
		if spec.code == code {
			return spec.perms
		}
	}
	return nil
}

// Run executa o seed.
func Run(ctx context.Context, d Deps) error {
	q := db.New(d.Pool)
	for _, name := range Departments {
		if _, err := q.UpsertDepartment(ctx, db.UpsertDepartmentParams{Name: name, IsFinalistic: d.Development}); err != nil {
			return fmt.Errorf("departamento %q: %w", name, err)
		}
	}
	for _, spec := range adminRoles() {
		if err := d.ensureAdminRole(ctx, q, spec); err != nil {
			return fmt.Errorf("role %q: %w", spec.code, err)
		}
	}
	// Quem já entrou pelo MT Login antes da role de proponente existir ficou sem nenhuma e vê "Aguardando liberação".
	if n, err := q.AssignRoleToAdminsWithoutRoles(ctx, RoleProponent); err != nil {
		return fmt.Errorf("role de proponente para quem não tem nenhuma: %w", err)
	} else if n > 0 {
		d.Log.Info("role de proponente atribuída a administradores sem nenhuma role", "quantidade", n)
	}
	if err := d.ensureSuperAdmins(ctx); err != nil {
		return err
	}
	if d.Development {
		if err := d.devUsers(ctx, q); err != nil {
			return err
		}
	}
	// Avisa instâncias em execução (LISTEN/NOTIFY) que as permissões podem ter mudado.
	return d.Engine.Invalidate(ctx)
}

func (d Deps) ensureAdminRole(ctx context.Context, q *db.Queries, spec roleSpec) error {
	exists, err := q.AdminRoleCodeExists(ctx, spec.code)
	if err != nil {
		return err
	}
	role, err := q.UpsertAdminRole(ctx, db.UpsertAdminRoleParams{Code: spec.code, Name: spec.name, Description: spec.description, IsSystem: spec.system})
	if err != nil {
		return err
	}
	if len(spec.perms) == 0 || (exists && !spec.topUp) {
		return nil
	}
	want := spec.perms
	if exists {
		// Role já existente: só as permissões padrão cuja ação a role ainda não tem.
		have, err := q.ListAdminPermissionsByRole(ctx, role.ID)
		if err != nil {
			return err
		}
		has := map[string]bool{}
		for _, h := range have {
			has[h.Action] = true
		}
		want = nil
		for _, p := range spec.perms {
			if !has[p.Action] {
				want = append(want, p)
			}
		}
		if len(want) == 0 {
			return nil
		}
	}
	// Aplica as permissões padrão, validadas pelo mesmo caminho da API.
	perms, issues := d.Engine.NormalizePermissions(want)
	if len(issues) > 0 {
		return fmt.Errorf("permissões padrão inválidas: %v", issues)
	}
	return database.WithTx(ctx, d.Pool, func(tx pgx.Tx) error {
		tq := q.WithTx(tx)
		for _, p := range perms {
			fields, err := marshalFields(p.Fields)
			if err != nil {
				return err
			}
			conds, _ := json.Marshal(p.Conditions)
			props, _ := json.Marshal(p.Properties)
			var subject *string
			if p.Subject != "" {
				subject = &p.Subject
			}
			if _, err := tq.CreateAdminPermission(ctx, db.CreateAdminPermissionParams{RoleID: role.ID, Action: p.Action, Subject: subject, Fields: fields, Conditions: conds, Properties: props}); err != nil {
				return err
			}
		}
		return nil
	})
}

func marshalFields(f []string) ([]byte, error) {
	if f == nil {
		return nil, nil
	}
	return json.Marshal(f)
}

// DevUser é um administrador de exemplo do ambiente de desenvolvimento. O CPF é fictício (válido,
// mas de ninguém): ninguém consegue autenticar com ele no MT Login.
type DevUser struct {
	CPF, Email, First, Last, Role, Department string
}

// DevUsers são os administradores criados pelo seed em desenvolvimento, um por role.
var DevUsers = []DevUser{
	{"11144477735", "super@dev.local", "Super", "Admin", RoleSuperAdmin, ""},
	{"44455566619", "revisor@dev.local", "Regina", "Revisora", RoleReviewer, "Financeiro"},
	{"88899900078", "revisor.juridico@dev.local", "Rui", "Revisor", RoleReviewer, "Jurídico"},
	{"55566677720", "analista@dev.local", "Ana", "Analista", RoleAnalyst, ""},
	{"66677788830", "supervisor@dev.local", "Sérgio", "Supervisor", RoleSupervisor, ""},
	{"77788899941", "secretario@dev.local", "Sílvia", "Secretária", RoleSecretary, ""},
}

func (d Deps) devUsers(ctx context.Context, q *db.Queries) error {
	for _, u := range DevUsers {
		if err := d.devAdmin(ctx, q, u); err != nil {
			return fmt.Errorf("usuário de desenvolvimento %s: %w", u.Email, err)
		}
	}
	return nil
}

func (d Deps) ensureSuperAdmins(ctx context.Context) error {
	svc := admin.NewService(d.Pool, nil)
	for _, c := range d.SuperAdminCPFs {
		if strings.TrimSpace(c) == "" {
			continue
		}
		a, err := svc.EnsureSuperAdmin(ctx, admin.EnsureSuperAdminInput{CPF: c}, "")
		if err != nil {
			return fmt.Errorf("super admin %s: %w", maskInput(c), err)
		}
		d.Log.Info("Super Admin garantido; o nome e o e-mail são preenchidos no primeiro acesso", "id", a.ID, "cpf", maskedCPF(a.Cpf))
	}
	return nil
}

func maskedCPF(c *string) string {
	if c == nil {
		return ""
	}
	return cpf.Mask(*c)
}

// maskInput esconde um CPF digitado (possivelmente inválido) para mensagens de erro.
func maskInput(s string) string {
	if digits, err := cpf.Normalize(s); err == nil {
		return cpf.Mask(digits)
	}
	return "(CPF inválido)"
}

func (d Deps) devAdmin(ctx context.Context, q *db.Queries, u DevUser) error {
	if _, err := q.GetAdminByCPF(ctx, &u.CPF); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	role, err := q.UpsertAdminRole(ctx, db.UpsertAdminRoleParams{Code: u.Role})
	if err != nil {
		return err
	}
	var dept *uuid.UUID
	if u.Department != "" {
		rows, err := q.ListDepartmentsByName(ctx, []string{u.Department})
		if err != nil || len(rows) != 1 {
			return fmt.Errorf("departamento %q: %w", u.Department, err)
		}
		dept = &rows[0].ID
	}
	err = database.WithTx(ctx, d.Pool, func(tx pgx.Tx) error {
		tq := q.WithTx(tx)
		a, err := tq.CreateAdmin(ctx, db.CreateAdminParams{Cpf: &u.CPF, FirstName: u.First, LastName: u.Last, Email: u.Email, IsActive: true, DepartmentID: dept})
		if err != nil {
			return err
		}
		return tq.AssignAdminRole(ctx, db.AssignAdminRoleParams{AdminID: a.ID, RoleID: role.ID})
	})
	if err != nil {
		return err
	}
	d.Log.Info("administrador de desenvolvimento criado (CPF fictício, sem login pelo MT Login)", "role", u.Role, "email", u.Email, "cpf", cpf.Mask(u.CPF))
	return nil
}
