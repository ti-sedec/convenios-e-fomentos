import { queryOptions } from '@tanstack/react-query';
import { projectKeys } from '@/features/projects/api/queries';
import { getQueueOverview } from './service';
import type { QueueScope } from './types';

// Sob `projectKeys.all`: toda ação sobre um projeto (assumir, conferir, devolver, indicar) já invalida a visão geral.
export const queueKeys = {
  overview: (scope: QueueScope) => [...projectKeys.all, 'queue-overview', scope] as const
};

export const queueOverviewQueryOptions = (scope: QueueScope = 'team') =>
  queryOptions({
    queryKey: queueKeys.overview(scope),
    queryFn: () => getQueueOverview(scope),
    staleTime: 30_000
  });
