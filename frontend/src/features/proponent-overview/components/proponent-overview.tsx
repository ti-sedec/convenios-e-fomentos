'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { parseAsStringLiteral, useQueryState } from 'nuqs';
import { Icons } from '@/components/icons';
import { QueryError } from '@/components/query-error';
import { buttonVariants } from '@/components/ui/button';
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle
} from '@/components/ui/empty';
import { Skeleton } from '@/components/ui/skeleton';
import { useCan } from '@/features/auth/session-provider';
import { accessApi, accessKeys } from '@/features/projects/access-filter';
import { profileQueryOptions } from '@/features/projects/api/queries';
import { projectActions } from '@/features/projects/api/types';
import { AccessFilter } from '@/features/projects/components/access-filter';
import { NewProjectButton } from '@/features/projects/components/new-project-button';
import { overviewQueryOptions } from '../api/queries';
import { ActivityFeed } from './activity-feed';
import { AttentionList } from './attention-list';
import { RecentProjects } from './recent-projects';
import { StatusCounters } from './status-counters';

/** Visão geral do proponente: o que pede ação, o panorama por situação, os projetos recentes e a atividade. */
export function ProponentOverview({ name }: { name: string }) {
  const can = useCan();
  const [acesso, setAcesso] = useQueryState(
    'acesso',
    parseAsStringLiteral(accessKeys).withDefault('todos').withOptions({ clearOnDefault: true })
  );
  const overview = useQuery(overviewQueryOptions(accessApi(acesso)));
  const profile = useQuery({ ...profileQueryOptions(), enabled: can(projectActions.profileRead) });

  const by = overview.data?.byAccess;
  const noProjects = by !== undefined && by.owner + by.member === 0;

  return (
    <div className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground'>Olá, {name}.</p>
        {!noProjects && (
          <AccessFilter value={acesso} onChange={(k) => void setAcesso(k)} totals={by} />
        )}
      </div>

      {overview.isError ? (
        <QueryError error={overview.error} what='o resumo dos seus projetos' />
      ) : overview.isLoading || !overview.data ? (
        <div className='space-y-4'>
          <Skeleton className='h-32 w-full' />
          <Skeleton className='h-24 w-full' />
        </div>
      ) : noProjects ? (
        <Empty className='border'>
          <EmptyHeader>
            <EmptyTitle>Você ainda não tem projetos</EmptyTitle>
            <EmptyDescription>
              Comece pelo seu perfil de proponente e depois cadastre o seu projeto de convênio ou de
              termo de fomento. Projetos que outras pessoas compartilharem com você também aparecem
              aqui.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <div className='flex flex-wrap justify-center gap-2'>
              <Link
                href='/dashboard/perfil-proponente'
                className={buttonVariants({ variant: 'outline' })}
              >
                <Icons.profile aria-hidden /> Perfil de proponente
              </Link>
              <NewProjectButton />
            </div>
          </EmptyContent>
        </Empty>
      ) : (
        <>
          <AttentionList
            items={overview.data.attention}
            profileIncomplete={profile.data ? !profile.data.complete : false}
          />
          <StatusCounters overview={overview.data} acesso={acesso} />
          <div className='grid gap-4 lg:grid-cols-2'>
            <RecentProjects acesso={acesso} />
            <ActivityFeed acesso={acesso} />
          </div>
        </>
      )}
    </div>
  );
}
