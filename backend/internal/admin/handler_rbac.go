package admin

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/auth"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

const (
	defaultPageSize = 25
	maxPageSize     = 100
)

func (h *Handler) actor(r *http.Request) Actor {
	p, _ := auth.AdminFrom(r.Context())
	return ActorOf(p, httpapi.ClientIP(r.Context()))
}

func idParam(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, httpapi.ErrNotFound()
	}
	return id, nil
}

// guard autentica e exige a permissão informada.
func (h *Handler) guard(action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return h.auth.RequireAdmin(RequirePermission(h.engine, action)(next))
	}
}

// rbacRoutes registra administradores, roles e catálogo.
func (h *Handler) rbacRoutes(r chi.Router) {
	r.With(h.guard("admin::settings.users.read")).Get("/admin/users", h.listUsers)
	r.With(h.guard("admin::settings.users.delete")).Post("/admin/users/batch-delete", h.batchDeleteUsers)
	r.With(h.guard("admin::settings.users.read")).Get("/admin/users/{id}", h.getUser)
	r.With(h.guard("admin::settings.users.update")).Put("/admin/users/{id}", h.updateUser)
	r.With(h.guard("admin::settings.users.delete")).Delete("/admin/users/{id}", h.deleteUser)
	r.With(h.guard("admin::settings.users.update")).Post("/admin/users/{id}/activate", h.setActive(true))
	r.With(h.guard("admin::settings.users.update")).Post("/admin/users/{id}/deactivate", h.setActive(false))
	r.With(h.guard("admin::settings.users.update")).Post("/admin/users/{id}/block", h.setBlocked(true))
	r.With(h.guard("admin::settings.users.update")).Post("/admin/users/{id}/unblock", h.setBlocked(false))
	r.With(h.guard("admin::settings.users.read")).Get("/admin/users/{id}/permissions", h.getUserPermissions)
	r.With(h.guard("admin::settings.users.update")).Put("/admin/users/{id}/roles", h.setUserRoles)
	r.With(h.guard("admin::settings.users.update")).Post("/admin/users/{id}/roles", h.addUserRoles)
	r.With(h.guard("admin::settings.users.update")).Delete("/admin/users/{id}/roles/{roleId}", h.removeUserRole)

	r.With(h.GuardAny("admin::settings.roles.read", "admin::settings.users.read")).Get("/admin/roles", h.listRoles)
	r.With(h.guard("admin::settings.roles.create")).Post("/admin/roles", h.createRole)
	r.With(h.guard("admin::settings.roles.read")).Get("/admin/roles/{id}", h.getRole)
	r.With(h.guard("admin::settings.roles.update")).Put("/admin/roles/{id}", h.updateRole)
	r.With(h.guard("admin::settings.roles.delete")).Delete("/admin/roles/{id}", h.deleteRole)
	r.With(h.guard("admin::settings.roles.update")).Put("/admin/roles/{id}/permissions", h.replacePermissions)
	r.With(h.guard("admin::settings.roles.create")).Post("/admin/roles/{id}/duplicate", h.duplicateRole)

	r.With(h.guard("admin::settings.roles.read")).Get("/admin/catalog/actions", h.catalogActions)
	r.With(h.guard("admin::settings.roles.read")).Get("/admin/catalog/conditions", h.catalogConditions)
}

// Guard expõe o middleware autenticação + permissão para outros pacotes (ex.: auditoria).
func (h *Handler) Guard(action string) func(http.Handler) http.Handler { return h.guard(action) }

// GuardAny é como Guard, mas basta ter uma das ações.
func (h *Handler) GuardAny(actions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return h.auth.RequireAdmin(RequireAnyPermission(h.engine, actions...)(next))
	}
}

// ---- administradores ----

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.ParsePage(r, defaultPageSize, maxPageSize)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	sort, err := httpapi.ParseSort(r, UserSortColumns, httpapi.Sort{Column: "a.created_at", Desc: true})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	q := r.URL.Query()
	f := ListUsersFilter{Q: q.Get("q"), Status: q.Get("status"), Sort: sort, Page: page}
	if f.Status != "" && !slices.Contains(UserStatuses, f.Status) {
		httpapi.Error(w, r, fieldIssue("status", "Valores aceitos: active, awaiting, inactive, pending, blocked."))
		return
	}
	if v := q.Get("roleId"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			httpapi.Error(w, r, fieldIssue("roleId", "UUID inválido."))
			return
		}
		f.RoleID = &id
	}
	list, total, err := h.svc.ListUsers(r.Context(), f)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, list, page.Meta(total))
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in struct {
		DepartmentID *uuid.UUID `json:"departmentId"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.UpdateUser(r.Context(), h.actor(r), id, UpdateUserInput{DepartmentID: in.DepartmentID})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) setActive(active bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r, "id")
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		v, err := h.svc.SetActive(r.Context(), h.actor(r), id, active)
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, v, nil)
	}
}

func (h *Handler) setBlocked(blocked bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r, "id")
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		v, err := h.svc.SetBlocked(r.Context(), h.actor(r), id, blocked)
		if err != nil {
			httpapi.Error(w, r, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, v, nil)
	}
}

func (h *Handler) getUserPermissions(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.GetUserPermissions(r.Context(), id)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) setUserRoles(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in struct {
		RoleIDs []uuid.UUID `json:"roleIds" validate:"required,min=1"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.SetRoles(r.Context(), h.actor(r), id, in.RoleIDs)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteUsers(r.Context(), h.actor(r), []uuid.UUID{id}); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) batchDeleteUsers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []uuid.UUID `json:"ids" validate:"required,min=1"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteUsers(r.Context(), h.actor(r), in.IDs); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addUserRoles(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in struct {
		RoleIDs []uuid.UUID `json:"roleIds" validate:"required,min=1"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.AddRoles(r.Context(), h.actor(r), id, in.RoleIDs)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) removeUserRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	roleID, err := idParam(r, "roleId")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.RemoveRole(r.Context(), h.actor(r), id, roleID)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// ---- roles ----

type roleInput struct {
	Name        string `json:"name" validate:"required,max=100"`
	Description string `json:"description" validate:"max=500"`
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListRoles(r.Context())
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, list, nil)
}

func (h *Handler) getRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.GetRole(r.Context(), id)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	var in roleInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.CreateRole(r.Context(), h.actor(r), in.Name, in.Description)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, v, nil)
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in roleInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.UpdateRole(r.Context(), h.actor(r), id, in.Name, in.Description)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteRole(r.Context(), h.actor(r), id); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) duplicateRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.DuplicateRole(r.Context(), h.actor(r), id)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, v, nil)
}

func (h *Handler) replacePermissions(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id")
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in struct {
		Permissions []struct {
			Action     string         `json:"action" validate:"required"`
			Subject    string         `json:"subject"`
			Fields     []string       `json:"fields"` // null = todos os campos
			Conditions []string       `json:"conditions"`
			Properties map[string]any `json:"properties"`
		} `json:"permissions" validate:"required,dive"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	perms := make([]authz.Permission, len(in.Permissions))
	for i, p := range in.Permissions {
		perms[i] = authz.Permission{Action: p.Action, Subject: p.Subject, Fields: p.Fields, Conditions: p.Conditions, Properties: p.Properties}
	}
	v, err := h.svc.ReplacePermissions(r.Context(), h.actor(r), id, perms)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// ---- catálogo ----

type actionView struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Category           string   `json:"category"`
	Resource           string   `json:"resource,omitempty"`
	SupportsFields     bool     `json:"supportsFields"`
	SupportsConditions bool     `json:"supportsConditions"`
	SupportsLocales    bool     `json:"supportsLocales"`
	AvailableFields    []string `json:"availableFields,omitempty"`
}

func (h *Handler) catalogActions(w http.ResponseWriter, r *http.Request) {
	type group struct {
		Category authz.Category `json:"category"`
		Actions  []actionView   `json:"actions"`
	}
	groups := []group{}
	for _, g := range h.engine.Actions().ByCategory(authz.DomainAdmin) {
		views := make([]actionView, 0, len(g.Actions))
		for _, a := range g.Actions {
			v := actionView{ID: a.ID, Name: a.Name, Category: string(a.Category), Resource: a.Resource, SupportsFields: a.SupportsFields,
				SupportsConditions: a.SupportsConditions, SupportsLocales: a.SupportsLocales}
			if s, ok := h.engine.Actions().Schema(a.Resource); ok && a.SupportsFields {
				v.AvailableFields = s.Configurable()
			}
			views = append(views, v)
		}
		groups = append(groups, group{Category: g.Category, Actions: views})
	}
	httpapi.JSON(w, http.StatusOK, groups, nil)
}

func (h *Handler) catalogConditions(w http.ResponseWriter, r *http.Request) {
	type condView struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	conds := h.engine.Conditions().All()
	out := make([]condView, len(conds))
	for i, c := range conds {
		out[i] = condView{ID: c.ID, Name: c.Name, Category: c.Category}
	}
	httpapi.JSON(w, http.StatusOK, out, nil)
}
