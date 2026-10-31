'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { Icons } from '@/components/icons';
import { QueryError } from '@/components/query-error';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { accessApi, type AccessKey } from '@/features/projects/access-filter';
import { archiveReasonLabels, type ArchiveReason } from '@/features/projects/api/types';
import { formatDateTimeCuiaba } from '@/features/projects/format';
import { projectHref } from '../format';
import { activityQueryOptions } from '../api/queries';
import type { ActivityBy, ActivityItem } from '../api/types';

const levelLabels = { read: 'leitura', edit: 'edição' } as const;
const FEED = 10;

const icons: Record<string, keyof typeof Icons> = {
  submitted: 'send',
  analysis_started: 'eye',
  complement_requested: 'warning',
  resubmitted: 'refresh',
  awaiting_opinion: 'circleCheck',
  archived: 'ban',
  shared_with_you: 'share'
};

/** O que aconteceu, escrito para o proponente. Um tipo que o frontend ainda não conhece vira um texto genérico. */
function describe(it: ActivityItem): string {
  const d = it.data;
  switch (it.kind) {
    case 'submitted':
      return d.late ? 'Projeto enviado (fora do prazo).' : 'Projeto enviado.';
    case 'analysis_started':
      return 'A equipe iniciou a análise.';
    case 'complement_requested':
      return [
        'A equipe pediu correções',
        d.items ? ` (${d.items} ${d.items === 1 ? 'item' : 'itens'})` : '',
        d.dueAt ? `, com prazo até ${formatDateTimeCuiaba(d.dueAt)}` : '',
        '.'
      ].join('');
    case 'resubmitted':
      return d.version ? `Correções enviadas (versão ${d.version}).` : 'Correções enviadas.';
    case 'awaiting_opinion':
      return 'Projeto enquadrado. Aguardando parecer.';
    case 'archived': {
      const reason = d.reason ? archiveReasonLabels[d.reason as ArchiveReason] : undefined;
      return reason ? `Projeto arquivado: ${reason}.` : 'Projeto arquivado.';
    }
    case 'shared_with_you':
      return d.level
        ? `Projeto compartilhado com você (acesso de ${levelLabels[d.level]}).`
        : 'Projeto compartilhado com você.';
    default:
      return 'Atualização no projeto.';
  }
}

/** Só quem o próprio proponente conhece: ele mesmo ou alguém do projeto. A equipe e o sistema não precisam de rótulo. */
const byLabels: Partial<Record<ActivityBy, string>> = {
  you: 'por você',
  member: 'por outro membro do projeto'
};

/** Os últimos marcos dos projetos, do mais novo ao mais antigo. */
export function ActivityFeed({ acesso }: { acesso: AccessKey }) {
  const feed = useQuery(activityQueryOptions(accessApi(acesso), FEED));
  const items = feed.data ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>Atividade recente</CardTitle>
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
              const by = byLabels[it.by];
              return (
                <li key={it.id} className='flex items-start gap-2 py-2'>
                  <Icon aria-hidden className='mt-0.5 size-4 shrink-0' />
                  <div className='min-w-0'>
                    <p className='text-sm'>{describe(it)}</p>
                    <p className='text-muted-foreground text-xs'>
                      <Link
                        href={projectHref(it.projectId)}
                        className='underline-offset-4 hover:underline'
                      >
                        {it.title || 'Projeto sem título'}
                      </Link>
                      {it.code && <span className='font-mono'> · {it.code}</span>}
                      {' · '}
                      {formatDateTimeCuiaba(it.at)}
                      {by && ` · ${by}`}
                    </p>
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
