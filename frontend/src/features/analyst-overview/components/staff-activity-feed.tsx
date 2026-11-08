'use client';

import { useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { Icons } from '@/components/icons';
import { QueryError } from '@/components/query-error';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { archiveReasonLabels, type ArchiveReason } from '@/features/projects/api/types';
import { formatDateTimeCuiaba } from '@/features/projects/format';
import { queueActivityQueryOptions } from '../api/queries';
import type { QueueActivityItem, QueueActor } from '../api/types';
import { scopeApi, type ScopeKey } from './scope-filter';

const FEED = 15;

const icons: Record<string, keyof typeof Icons> = {
  submitted: 'send',
  resubmitted: 'refresh',
  analysis_started: 'eye',
  analysis_taken_over: 'eye',
  complement_requested: 'warning',
  department_assigned: 'building',
  framing_confirmed: 'circleCheck',
  framing_refused: 'circleX',
  archived: 'ban'
};

/** Quem agiu, em texto: "Você", o nome de quem é da equipe, "O proponente" ou "O sistema". */
function who(by: QueueActor): string {
  if (by.self) return 'Você';
  if (by.type === 'proponent') return 'O proponente';
  if (by.type === 'system') return 'O sistema';
  return by.name || 'A equipe';
}

const inDept = (d: QueueActivityItem['data']) => (d.department ? ` (${d.department})` : '');

/** O que aconteceu, escrito para a equipe. Um tipo que o frontend ainda não conhece vira um texto genérico. */
function describe(it: QueueActivityItem): string {
  const d = it.data;
  const by = who(it.by);
  switch (it.kind) {
    case 'submitted':
      return d.late ? 'Novo projeto enviado (fora do prazo).' : 'Novo projeto enviado.';
    case 'resubmitted':
      return d.version
        ? `O proponente reenviou as correções (versão ${d.version}).`
        : 'O proponente reenviou as correções.';
    case 'analysis_started':
      return `${by} assumiu a análise.`;
    case 'analysis_taken_over':
      return `${by} assumiu o projeto de outro analista.`;
    case 'complement_requested':
      return [
        `${by} devolveu para correção`,
        d.items ? ` (${d.items} ${d.items === 1 ? 'item' : 'itens'})` : '',
        d.dueAt ? `, com prazo até ${formatDateTimeCuiaba(d.dueAt)}` : '',
        '.'
      ].join('');
    case 'department_assigned':
      return d.department
        ? `${by} indicou o departamento ${d.department}.`
        : `${by} indicou um departamento.`;
    case 'framing_confirmed':
      return `${by} confirmou o enquadramento${inDept(d)}.`;
    case 'framing_refused':
      return `${by} recusou o enquadramento${inDept(d)}.`;
    case 'archived': {
      const reason = d.reason ? archiveReasonLabels[d.reason as ArchiveReason] : undefined;
      const prefix =
        it.by.type === 'system' ? 'Arquivado pelo sistema' : `${by} arquivou o projeto`;
      return reason ? `${prefix}: ${reason}.` : `${prefix}.`;
    }
    default:
      return 'Atualização no projeto.';
  }
}

/** Os últimos marcos dos projetos da fila, do mais novo ao mais antigo. Mostra o que é interno: quem lê é a própria equipe. */
export function StaffActivityFeed({ escopo }: { escopo: ScopeKey }) {
  const feed = useQuery(queueActivityQueryOptions(scopeApi(escopo), FEED));
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
              return (
                <li key={it.id} className='flex items-start gap-2 py-2'>
                  <Icon aria-hidden className='mt-0.5 size-4 shrink-0' />
                  <div className='min-w-0'>
                    <p className='text-sm'>{describe(it)}</p>
                    <p className='text-muted-foreground text-xs'>
                      <Link
                        href={`/dashboard/projetos/${it.projectId}`}
                        className='underline-offset-4 hover:underline'
                      >
                        {it.title || 'Projeto sem título'}
                      </Link>
                      {it.code && <span className='font-mono'> · {it.code}</span>}
                      {' · '}
                      {formatDateTimeCuiaba(it.at)}
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
