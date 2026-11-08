import { queryOptions } from '@tanstack/react-query';
import { projectKeys } from '@/features/projects/api/queries';
import { ApiError } from '@/lib/api/errors';
import { getReviewerActivity, getReviewerMySummary, getReviewerOverview } from './service';

// Sob `projectKeys.all`: confirmar ou recusar um enquadramento (e qualquer outra ação sobre um projeto) já invalida a visão geral.
export const reviewerKeys = {
  overview: () => [...projectKeys.all, 'reviewer-overview'] as const,
  activity: (limit?: number) => [...projectKeys.all, 'reviewer-activity', limit ?? 15] as const,
  me: () => [...projectKeys.all, 'reviewer-me'] as const
};

// 403 é "sem departamento no cadastro": repetir não muda nada.
const retry = (count: number, error: unknown) =>
  !(error instanceof ApiError && error.status === 403) && count < 3;

export const reviewerOverviewQueryOptions = () =>
  queryOptions({
    queryKey: reviewerKeys.overview(),
    queryFn: getReviewerOverview,
    staleTime: 30_000,
    retry
  });

export const reviewerActivityQueryOptions = (limit?: number) =>
  queryOptions({
    queryKey: reviewerKeys.activity(limit),
    queryFn: () => getReviewerActivity(limit),
    staleTime: 30_000,
    retry
  });

export const reviewerMySummaryQueryOptions = () =>
  queryOptions({
    queryKey: reviewerKeys.me(),
    queryFn: getReviewerMySummary,
    staleTime: 30_000,
    retry
  });
