'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { QueryError } from '@/components/query-error';
import { Badge } from '@/components/ui/badge';
import { buttonVariants } from '@/components/ui/button';
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { accessApi, type AccessKey } from '@/features/projects/access-filter';
import { projectsQueryOptions } from '@/features/projects/api/queries';
import {
  accessLabels,
  archiveReasonLabels,
  stageLabels,
  statusLabels,
  type ProjectSummary
} from '@/features/projects/api/types';
import { formatBrl } from '@/features/projects/format';
import { projectHref, projectsHref } from '../format';

const RECENT = 5;

function action(p: ProjectSummary) {
  if (p.status === 'draft') return 'Continuar';
  if (p.status === 'submitted' && p.stage === 'awaiting_proponent') return 'Responder';
  return 'Abrir';
}

/** Em que pé está o projeto, em texto. */
function situationOf(p: ProjectSummary) {
  if (p.status === 'submitted' && p.stage) return stageLabels[p.stage];
  if (p.status === 'archived' && p.archiveReason) return archiveReasonLabels[p.archiveReason];
  return statusLabels[p.status];
}

/** Os projetos alterados há menos tempo, cada um com uma ação principal. */
export function RecentProjects({ acesso }: { acesso: AccessKey }) {
  const list = useQuery(projectsQueryOptions({ access: accessApi(acesso), pageSize: RECENT }));
  const rows = list.data?.data ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Projetos recentes</CardTitle>
      </CardHeader>
      <CardContent>
        {list.isError ? (
          <QueryError error={list.error} what='os projetos' />
        ) : list.isLoading ? (
          <Skeleton className='h-48 w-full' />
        ) : rows.length === 0 ? (
          <p className='text-muted-foreground text-sm'>Nenhum projeto neste filtro.</p>
        ) : (
          <ul className='divide-y'>
            {rows.map((p) => (
              <li key={p.id} className='flex flex-wrap items-center justify-between gap-2 py-2'>
                <div className='min-w-0'>
                  <Link
                    href={projectHref(p.id)}
                    className='text-sm font-medium underline-offset-4 hover:underline'
                  >
                    {p.title || 'Projeto sem título'}
                  </Link>
                  <p className='text-muted-foreground text-xs'>
                    {[p.institutionName, p.totalCents ? formatBrl(p.totalCents) : '']
                      .filter(Boolean)
                      .join(' · ')}
                  </p>
                  <div className='mt-1 flex flex-wrap items-center gap-1'>
                    <Badge variant={p.status === 'archived' ? 'outline' : 'secondary'}>
                      {situationOf(p)}
                    </Badge>
                    {p.access && p.access !== 'owner' && (
                      <Badge variant='outline'>Compartilhado · {accessLabels[p.access]}</Badge>
                    )}
                  </div>
                </div>
                <Link
                  href={projectHref(p.id)}
                  className={buttonVariants({ size: 'sm', variant: 'outline' })}
                >
                  {action(p)}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
      <CardFooter>
        <Link
          href={projectsHref(null, acesso)}
          className='text-sm font-medium underline-offset-4 hover:underline'
        >
          Ver todos os projetos
        </Link>
      </CardFooter>
    </Card>
  );
}
