import { queryOptions } from '@tanstack/react-query';
import { projectKeys } from '@/features/projects/api/queries';
import { getActivity, getOverview } from './service';
import type { AccessFilter } from './types';

// Sob `projectKeys.all`: toda mutação de projeto (envio, correção, exclusão) já invalida a visão geral.
export const overviewKeys = {
  overview: (access?: AccessFilter) => [...projectKeys.all, 'overview', access ?? 'all'] as const,
  activity: (access?: AccessFilter, limit?: number) =>
    [...projectKeys.all, 'activity', access ?? 'all', limit ?? 10] as const
};

export const overviewQueryOptions = (access?: AccessFilter) =>
  queryOptions({
    queryKey: overviewKeys.overview(access),
    queryFn: () => getOverview(access),
    staleTime: 30_000
  });

export const activityQueryOptions = (access?: AccessFilter, limit?: number) =>
  queryOptions({
    queryKey: overviewKeys.activity(access, limit),
    queryFn: () => getActivity(access, limit),
    staleTime: 30_000
  });
