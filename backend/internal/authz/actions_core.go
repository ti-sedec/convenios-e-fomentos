package authz

// crud monta as ações create/read/update/delete de um recurso de configuração.
func crud(resource, label string, cat Category, verbs ...string) []Action {
	names := map[string]string{"create": "Criar", "read": "Ler", "update": "Atualizar", "delete": "Excluir"}
	var out []Action
	for _, v := range verbs {
		out = append(out, Action{
			ID:       "admin::" + resource + "." + v,
			Domain:   DomainAdmin,
			Category: cat,
			Name:     names[v] + " " + label,
			Resource: resource,
		})
	}
	return out
}

// RegisterCoreActions registra as ações administrativas fixas do sistema
// (configurações, plugins e aplicação). As ações de conteúdo ficam com cada recurso.
func RegisterCoreActions(r *Registry) {
	all := []string{"create", "read", "update", "delete"}
	r.MustRegister(crud("settings.users", "administradores", CategorySettings, all...)...)
	r.MustRegister(crud("settings.roles", "roles de administrador", CategorySettings, all...)...)
	r.MustRegister(crud("settings.departments", "departamentos", CategorySettings, all...)...)
	r.MustRegister(crud("settings.audit-logs", "registros de auditoria", CategorySettings, "read")...)
}
