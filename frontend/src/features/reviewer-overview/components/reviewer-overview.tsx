'use client';

import { useQuery } from '@tanstack/react-query';
import { QueryError } from '@/components/query-error';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { ApiError } from '@/lib/api/errors';
import { reviewerOverviewQueryOptions } from '../api/queries';
import { DepartmentActivityFeed } from './department-activity-feed';
import { DepartmentCounters } from './department-counters';
import { MyDecisions } from './my-decisions';
import { NoDepartment } from './no-department';
import { PendingDecisions } from './pending-decisions';

/** Visão geral do Revisor do Departamento: o que espera a decisão dele e o panorama do departamento. */
export function ReviewerOverview({
  name,
  hasDepartment
}: {
  name: string;
  hasDepartment: boolean;
}) {
  // Sem departamento no cadastro a API nega (403); a sessão já diz isso, então nem se pergunta.
  const overview = useQuery({ ...reviewerOverviewQueryOptions(), enabled: hasDepartment });

  if (!hasDepartment || (overview.error instanceof ApiError && overview.error.status === 403)) {
    return (
      <div className='space-y-6'>
        <p className='text-muted-foreground'>Olá, {name}.</p>
        <NoDepartment />
      </div>
    );
  }
  return (
    <div className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground'>Olá, {name}.</p>
        {overview.data?.department && (
          <Badge variant='outline'>Departamento: {overview.data.department.name}</Badge>
        )}
      </div>
      {overview.isError ? (
        <QueryError error={overview.error} what='o resumo do departamento' />
      ) : overview.isLoading || !overview.data ? (
        <div className='space-y-4'>
          <Skeleton className='h-48 w-full' />
          <Skeleton className='h-24 w-full' />
        </div>
      ) : (
        <>
          <PendingDecisions overview={overview.data} />
          <DepartmentCounters counts={overview.data.counts} />
          <div className='grid gap-4 lg:grid-cols-2'>
            <DepartmentActivityFeed />
            <MyDecisions />
          </div>
        </>
      )}
    </div>
  );
}
