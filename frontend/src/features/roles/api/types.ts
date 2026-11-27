/** Ações do backend usadas para esconder elementos de UI (a autorização real é do backend). */
export const roleActions = {
  read: 'admin::settings.roles.read',
  create: 'admin::settings.roles.create',
  update: 'admin::settings.roles.update',
  delete: 'admin::settings.roles.delete'
} as const;

export interface RoleSummary {
  id: string;
  code: string;
  name: string;
  description: string;
  isSystem: boolean;
  /** Acesso total; não tem permissões editáveis. */
  isSuperAdmin: boolean;
  userCount: number;
  createdAt: string;
  updatedAt: string;
}

/** Permissão de uma role. `fields: null` significa todos os campos; várias condições combinam com OR. */
export interface PermissionView {
  action: string;
  subject?: string;
  fields: string[] | null;
  conditions: string[];
  properties?: Record<string, unknown>;
}

export interface RoleDetail extends RoleSummary {
  permissions: PermissionView[];
}

export interface RoleInput {
  name: string;
  description: string;
}

export interface CatalogAction {
  id: string;
  name: string;
  category: string;
  resource: string;
  supportsFields: boolean;
  supportsConditions: boolean;
  supportsLocales: boolean;
  /** Campos configuráveis (só em ações que suportam campos). */
  availableFields?: string[];
}

export interface CatalogGroup {
  category: string;
  actions: CatalogAction[];
}

export interface CatalogCondition {
  id: string;
  name: string;
  category: string;
}
