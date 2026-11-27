package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// DBStore lê permissões e membros de roles do PostgreSQL.
type DBStore struct{ Q *db.Queries }

// RolePermissions implementa Store.
func (s DBStore) RolePermissions(ctx context.Context, roleID uuid.UUID) ([]Permission, error) {
	rows, err := s.Q.ListAdminPermissionsByRole(ctx, roleID)
	if err != nil {
		return nil, err
	}
	out := make([]Permission, 0, len(rows))
	for _, r := range rows {
		p := Permission{Action: r.Action}
		if r.Subject != nil {
			p.Subject = *r.Subject
		}
		if r.Fields != nil {
			p.Fields = []string{}
			if err := json.Unmarshal(r.Fields, &p.Fields); err != nil {
				return nil, fmt.Errorf("permissão %s: fields inválido: %w", r.ID, err)
			}
		}
		if err := json.Unmarshal(r.Conditions, &p.Conditions); err != nil {
			return nil, fmt.Errorf("permissão %s: conditions inválido: %w", r.ID, err)
		}
		if err := json.Unmarshal(r.Properties, &p.Properties); err != nil {
			return nil, fmt.Errorf("permissão %s: properties inválido: %w", r.ID, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// AdminIDsWithAnyRole implementa RoleMembers.
func (s DBStore) AdminIDsWithAnyRole(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	return s.Q.ListAdminIDsSharingRoles(ctx, roleIDs)
}

// SyncActions remove das roles as permissões de ações que não existem mais no
// código (registradas no log como aviso) e invalida o cache.
func SyncActions(ctx context.Context, q *db.Queries, reg *Registry, eng *Engine, log *slog.Logger) error {
	ids := reg.IDs(DomainAdmin)
	if len(ids) == 0 {
		// Proteção: um registro vazio por engano apagaria todas as permissões.
		return fmt.Errorf("nenhuma ação de admin registrada; sincronização abortada")
	}
	removed, err := q.DeleteAdminPermissionsNotInActions(ctx, ids)
	if err != nil {
		return fmt.Errorf("sincronizando ações: %w", err)
	}
	for _, r := range removed {
		log.WarnContext(ctx, "permissão removida: ação não existe mais no código", "role_id", r.RoleID, "action", r.Action)
	}
	if len(removed) > 0 {
		return eng.Invalidate(ctx)
	}
	return nil
}
