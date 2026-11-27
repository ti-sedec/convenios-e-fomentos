import { apiClient } from '@/lib/api-client';
import type { ApiResponse } from '@/lib/api/types';
import type {
  CatalogCondition,
  CatalogGroup,
  PermissionView,
  RoleDetail,
  RoleInput,
  RoleSummary
} from './types';

const base = '/admin/roles';
const json = (body: unknown): RequestInit => ({ body: JSON.stringify(body) });

export async function listRoles() {
  return (await apiClient<ApiResponse<RoleSummary[]>>(base)).data;
}

export async function getRole(id: string) {
  return (await apiClient<ApiResponse<RoleDetail>>(`${base}/${id}`)).data;
}

export async function createRole(input: RoleInput) {
  return (await apiClient<ApiResponse<RoleDetail>>(base, { method: 'POST', ...json(input) })).data;
}

export async function updateRole(id: string, input: RoleInput) {
  return (
    await apiClient<ApiResponse<RoleDetail>>(`${base}/${id}`, { method: 'PUT', ...json(input) })
  ).data;
}

export async function deleteRole(id: string) {
  await apiClient<void>(`${base}/${id}`, { method: 'DELETE' });
}

export async function duplicateRole(id: string) {
  return (await apiClient<ApiResponse<RoleDetail>>(`${base}/${id}/duplicate`, { method: 'POST' }))
    .data;
}

/** Substitui, em transação, todas as permissões da role. */
export async function replacePermissions(id: string, permissions: PermissionView[]) {
  const body = permissions.map(({ action, subject, fields, conditions }) => ({
    action,
    subject,
    fields,
    conditions
  }));
  return (
    await apiClient<ApiResponse<RoleDetail>>(`${base}/${id}/permissions`, {
      method: 'PUT',
      ...json({ permissions: body })
    })
  ).data;
}

export async function getCatalogActions() {
  return (await apiClient<ApiResponse<CatalogGroup[]>>('/admin/catalog/actions')).data;
}

export async function getCatalogConditions() {
  return (await apiClient<ApiResponse<CatalogCondition[]>>('/admin/catalog/conditions')).data;
}
