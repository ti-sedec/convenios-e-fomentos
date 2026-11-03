'use client';

import { useQuery } from '@tanstack/react-query';
import { parseAsStringLiteral, useQueryState } from 'nuqs';
import { QueryError } from '@/components/query-error';
import { Skeleton } from '@/components/ui/skeleton';
import { queueOverviewQueryOptions } from '../api/queries';
import { MySummary } from './my-summary';
import { QueueCounters } from './queue-counters';
import { ScopeFilter, scopeApi, scopeKeys } from './scope-filter';
import { StaffActivityFeed } from './staff-activity-feed';
import { WorkBlocks } from './work-blocks';

/** Visão geral do Analista: o que pede a ação dele e o panorama da fila. */
export function AnalystOverview({ name }: { name: string }) {
  const [escopo, setEscopo] = useQueryState(
    'escopo',
    parseAsStringLiteral(scopeKeys).withDefault('equipe').withOptions({ clearOnDefault: true })
  );
  const overview = useQuery(queueOverviewQueryOptions(scopeApi(escopo)));

  return (
    <div className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground'>Olá, {name}.</p>
        <ScopeFilter value={escopo} onChange={(k) => void setEscopo(k)} />
      </div>
      {overview.isError ? (
        <QueryError error={overview.error} what='o resumo da fila' />
      ) : overview.isLoading || !overview.data ? (
        <div className='space-y-4'>
          <Skeleton className='h-24 w-full' />
          <Skeleton className='h-48 w-full' />
        </div>
      ) : (
        <>
          <WorkBlocks overview={overview.data} escopo={escopo} />
          <QueueCounters counts={overview.data.counts} escopo={escopo} />
          <div className='grid gap-4 lg:grid-cols-2'>
            <StaffActivityFeed escopo={escopo} />
            <MySummary />
          </div>
        </>
      )}
    </div>
  );
}
