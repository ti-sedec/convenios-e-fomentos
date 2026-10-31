import { apiClient } from '@/lib/api-client';
import type { ApiResponse } from '@/lib/api/types';
import type { AccessFilter, ActivityItem, Overview } from './types';

/** Sem `access` vale o padrão do servidor: tudo o que é da pessoa. */
const withAccess = (access?: AccessFilter, extra: Record<string, string> = {}) => {
  const params = new URLSearchParams(extra);
  if (access) params.set('access', access);
  const qs = params.toString();
  return qs ? `?${qs}` : '';
};

export async function getOverview(access?: AccessFilter) {
  return (await apiClient<ApiResponse<Overview>>(`/admin/projects/overview${withAccess(access)}`))
    .data;
}

export async function getActivity(access?: AccessFilter, limit?: number) {
  return (
    await apiClient<ApiResponse<ActivityItem[]>>(
      `/admin/projects/activity${withAccess(access, limit ? { limit: String(limit) } : {})}`
    )
  ).data;
}
