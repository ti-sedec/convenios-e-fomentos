'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { Icons } from '@/components/icons';
import { QueryError } from '@/components/query-error';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { archiveReasonLabels, type ArchiveReason } from '@/features/projects/api/types';
import { formatDateTimeCuiaba } from '@/features/projects/format';
import { reviewerActivityQueryOptions } from '../api/queries';
import type { ReviewerActivityItem, ReviewerActor } from '../api/types';

const FEED = 15;

const icons: Record<string, keyof typeof Icons> = {
  framing_indicated: 'building',
  framing_confirmed: 'circleCheck',
  framing_refused: 'circleX',
  archived: 'ban'
};

/** Quem agiu, em texto: "Você", o nome de quem é da equipe, "O proponente" ou "O sistema". */
function who(by: ReviewerActor): string {
  if (by.self) return 'Você';
  if (by.type === 'proponent') return 'O proponente';
  if (by.type === 'system') return 'O sistema';
  return by.name || 'A equipe';
}

/** O que aconteceu, escrito para o Revisor. Um tipo que o frontend ainda não conhece vira um texto genérico. */
function describe(it: ReviewerActivityItem): string {
  const by = who(it.by);
  switch (it.kind) {
    case 'framing_indicated':
      return it.data.round && it.data.round > 1
        ? `${by} indicou o projeto para o seu departamento (indicação nº ${it.data.round}).`
        : `${by} indicou o projeto para o seu departamento.`;
    case 'framing_confirmed':
      return `${by} confirmou o enquadramento.`;
    case 'framing_refused':
      return `${by} recusou o enquadramento.`;
    case 'archived': {
      const reason = it.data.reason
        ? archiveReasonLabels[it.data.reason as ArchiveReason]
        : undefined;
      const prefix =
        it.by.type === 'system' ? 'Arquivado pelo sistema' : `${by} arquivou o projeto`;
      return reason ? `${prefix}: ${reason}.` : `${prefix}.`;
    }
    default:
      return 'Atualização no projeto.';
  }
}

/**
 * Os últimos marcos do departamento, do mais novo ao mais antigo. Mostra os colegas pelo nome. Um projeto recusado sai do departamento,
 * então vem sem link (abri-lo daria "sem permissão").
 */
export function DepartmentActivityFeed() {
  const feed = useQuery(reviewerActivityQueryOptions(FEED));
  const items = feed.data ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Atividade do departamento</CardTitle>
      </CardHeader>
      <CardContent>
        {feed.isError ? (
          <QueryError error={feed.error} what='a atividade' />
        ) : feed.isLoading ? (
          <Skeleton className='h-48 w-full' />
        ) : items.length === 0 ? (
          <p className='text-muted-foreground text-sm'>Ainda não há atividade.</p>
        ) : (
          <ol className='divide-y'>
            {items.map((it) => {
              const Icon = Icons[icons[it.kind] ?? 'history'];
              return (
                <li key={it.id} className='flex items-start gap-2 py-2'>
                  <Icon aria-hidden className='mt-0.5 size-4 shrink-0' />
                  <div className='min-w-0'>
                    <p className='text-sm'>{describe(it)}</p>
                    <p className='text-muted-foreground text-xs'>
                      {it.linkable ? (
                        <Link
                          href={`/dashboard/projetos/${it.projectId}`}
                          className='underline-offset-4 hover:underline'
                        >
                          {it.title || 'Projeto sem título'}
                        </Link>
                      ) : (
                        <span>{it.title || 'Projeto sem título'}</span>
                      )}
                      {it.code && <span className='font-mono'> · {it.code}</span>}
                      {' · '}
                      {formatDateTimeCuiaba(it.at)}
                    </p>
                    {!it.linkable && (
                      <p className='text-muted-foreground text-xs'>
                        Voltou ao Analista; já não está no seu departamento.
                      </p>
                    )}
                  </div>
                </li>
              );
            })}
          </ol>
        )}
      </CardContent>
    </Card>
  );
}
