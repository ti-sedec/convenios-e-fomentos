import { apiClient } from '@/lib/api-client';
import type { ApiResponse } from '@/lib/api/types';
import type { QueueOverview, QueueScope } from './types';

/** Sem `scope` vale o padrão do servidor: toda a equipe. */
export async function getQueueOverview(scope?: QueueScope) {
  const qs = scope && scope !== 'team' ? `?scope=${scope}` : '';
  return (await apiClient<ApiResponse<QueueOverview>>(`/admin/projects/queue/overview${qs}`)).data;
}
