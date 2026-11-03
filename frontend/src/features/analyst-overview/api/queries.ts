import { queryOptions } from '@tanstack/react-query';
import { projectKeys } from '@/features/projects/api/queries';
import { getMySummary, getQueueActivity, getQueueOverview } from './service';
import type { QueueScope } from './types';

// Sob `projectKeys.all`: toda ação sobre um projeto (assumir, conferir, devolver, indicar) já invalida a visão geral.
export const queueKeys = {
  overview: (scope: QueueScope) => [...projectKeys.all, 'queue-overview', scope] as const,
  activity: (scope: QueueScope, limit?: number) =>
    [...projectKeys.all, 'queue-activity', scope, limit ?? 15] as const,
  me: () => [...projectKeys.all, 'queue-me'] as const
};

export const queueOverviewQueryOptions = (scope: QueueScope = 'team') =>
  queryOptions({
    queryKey: queueKeys.overview(scope),
    queryFn: () => getQueueOverview(scope),
    staleTime: 30_000
  });

export const queueActivityQueryOptions = (scope: QueueScope = 'team', limit?: number) =>
  queryOptions({
    queryKey: queueKeys.activity(scope, limit),
    queryFn: () => getQueueActivity(scope, limit),
    staleTime: 30_000
  });

export const mySummaryQueryOptions = () =>
  queryOptions({ queryKey: queueKeys.me(), queryFn: getMySummary, staleTime: 30_000 });
