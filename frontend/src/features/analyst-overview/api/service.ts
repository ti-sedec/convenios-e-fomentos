import { apiClient } from '@/lib/api-client';
import type { ApiResponse } from '@/lib/api/types';
import type { MySummary, QueueActivityItem, QueueOverview, QueueScope } from './types';

/** Sem `scope` vale o padrão do servidor: toda a equipe. */
const scopeQuery = (scope?: QueueScope, extra: Record<string, string> = {}) => {
  const params = new URLSearchParams(extra);
  if (scope && scope !== 'team') params.set('scope', scope);
  const qs = params.toString();
  return qs ? `?${qs}` : '';
};

export async function getQueueOverview(scope?: QueueScope) {
  return (
    await apiClient<ApiResponse<QueueOverview>>(
      `/admin/projects/queue/overview${scopeQuery(scope)}`
    )
  ).data;
}

export async function getQueueActivity(scope?: QueueScope, limit?: number) {
  return (
    await apiClient<ApiResponse<QueueActivityItem[]>>(
      `/admin/projects/queue/activity${scopeQuery(scope, limit ? { limit: String(limit) } : {})}`
    )
  ).data;
}

export async function getMySummary() {
  return (await apiClient<ApiResponse<MySummary>>('/admin/projects/queue/me')).data;
}
