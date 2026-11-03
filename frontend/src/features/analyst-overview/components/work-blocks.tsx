'use client';

import Link from 'next/link';
import { useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { buttonVariants } from '@/components/ui/button';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { dueLabel } from '@/features/proponent-overview/format';
import { formatDateTimeCuiaba, formatDuration } from '@/features/projects/format';
import type { BlockKey, Holder, QueueBlock, QueueItem, QueueOverview } from '../api/types';
import type { ScopeKey } from './scope-filter';

const SHOWN = 5;
const projectHref = (id: string) => `/dashboard/projetos/${id}`;

const holderLabels: Record<Holder, string> = {
  team: 'Com a equipe',
  analyst: 'Com o Analista',
  department: 'Com o departamento'
};

interface BlockSpec {
  key: BlockKey;
  title: string;
  note?: string;
  cta: string;
  /** A linha de detalhe de cada projeto: sempre em texto, nunca só cor. */
  detail: (it: QueueItem, now: number, overview: QueueOverview) => { text: string; late?: boolean };
}

const ago = (it: QueueItem) => formatDuration(it.sinceSeconds);

// Do mais urgente para o menos: o que voltou para o Analista, o que entra, o que destrava e o que está parado.
const specs: BlockSpec[] = [
  {
    key: 'refused',
    title: 'Recusados pelo Revisor',
    cta: 'Reindicar departamento',
    detail: (it) => ({
      text: `Recusado por ${it.department ?? 'departamento'}${it.refusedAt ? ` em ${formatDateTimeCuiaba(it.refusedAt)}` : ''}`
    })
  },
  {
    key: 'resubmitted',
    title: 'Reenviados pelo proponente',
    cta: 'Conferir',
    detail: (it) => ({ text: `Reenviado há ${ago(it)}; ainda sem conferência` })
  },
  {
    key: 'entry',
    title: 'Aguardando assumir',
    cta: 'Assumir',
    detail: (it, _now, o) => ({
      text: it.overLimit
        ? `Há ${ago(it)}, acima do limite de ${o.limits.awaiting_checklist} dias`
        : `Há ${ago(it)}`,
      late: it.overLimit
    })
  },
  {
    key: 'ready',
    title: 'Tudo aprovado, falta indicar o departamento',
    cta: 'Indicar departamento',
    detail: (it) => ({ text: `Conferido; em análise há ${ago(it)}` })
  },
  {
    key: 'stalled',
    title: 'Parados além do limite',
    cta: 'Abrir',
    detail: (it) => ({
      text: `${it.holder ? holderLabels[it.holder] : 'Parado'} há ${ago(it)} (limite de ${it.limitDays} dias)`,
      late: true
    })
  },
  {
    key: 'deadlines',
    title: 'Prazos de correção a vencer',
    note: 'Quem age é o proponente; só acompanhe.',
    cta: 'Abrir',
    detail: (it, now) => (it.dueAt ? dueLabel(it.dueAt, it.overdue, now) : { text: '' })
  }
];

function BlockCard({
  spec,
  block,
  overview,
  now,
  escopo
}: {
  spec: BlockSpec;
  block: QueueBlock;
  overview: QueueOverview;
  now: number;
  escopo: ScopeKey;
}) {
  const [all, setAll] = useState(false);
  const items = all ? block.items : block.items.slice(0, SHOWN);
  const total = `${block.total}${block.capped ? '+' : ''}`;
  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex flex-wrap items-center gap-2'>
          {spec.title}
          <Badge variant='secondary' className='tabular-nums'>
            {total}
          </Badge>
        </CardTitle>
        {(spec.note || block.capped || (spec.key === 'entry' && escopo === 'meus')) && (
          <p className='text-muted-foreground text-xs'>
            {[
              spec.note,
              block.capped ? 'Há mais projetos do que os examinados: o total é "ou mais".' : '',
              spec.key === 'entry' && escopo === 'meus'
                ? 'A fila de entrada não depende do filtro: ninguém é dono antes de assumir.'
                : ''
            ]
              .filter(Boolean)
              .join(' ')}
          </p>
        )}
      </CardHeader>
      <CardContent>
        <ul className='divide-y'>
          {items.map((it) => {
            const d = spec.detail(it, now, overview);
            return (
              <li
                key={it.projectId}
                className='flex flex-wrap items-center justify-between gap-2 py-2'
              >
                <div className='min-w-0'>
                  <Link
                    href={projectHref(it.projectId)}
                    className='text-sm font-medium underline-offset-4 hover:underline'
                  >
                    {it.title || 'Projeto sem título'}
                  </Link>
                  <p className='text-muted-foreground text-xs'>
                    {[it.code, it.institution, it.analyst ? `Analista: ${it.analyst.name}` : '']
                      .filter(Boolean)
                      .join(' · ')}
                  </p>
                  <p
                    className={`text-xs ${d.late ? 'text-destructive font-medium' : 'text-muted-foreground'}`}
                  >
                    {d.text}
                  </p>
                  {it.late && (
                    <Badge variant='outline' className='mt-1'>
                      Fora do prazo
                    </Badge>
                  )}
                </div>
                <Link
                  href={projectHref(it.projectId)}
                  className={buttonVariants({ size: 'sm', variant: 'outline' })}
                >
                  {spec.cta}
                </Link>
              </li>
            );
          })}
        </ul>
        {block.items.length > SHOWN && (
          <Button
            size='sm'
            variant='ghost'
            className='mt-2'
            aria-expanded={all}
            onClick={() => setAll((v) => !v)}
          >
            {all ? 'Mostrar menos' : `Mostrar mais (${block.items.length - SHOWN})`}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

/** "Precisa de você": um cartão por bloco, do mais urgente para o menos. Bloco vazio não aparece. */
export function WorkBlocks({ overview, escopo }: { overview: QueueOverview; escopo: ScopeKey }) {
  // Lido uma vez ao montar: o texto do prazo não depende de um relógio que mudaria entre renderizações.
  const [now] = useState(() => Date.now());
  const shown = specs.filter((s) => overview.blocks[s.key].total > 0);
  if (shown.length === 0) {
    return (
      <Card>
        <CardContent>
          <p className='text-muted-foreground text-sm'>Nada pede a sua ação agora.</p>
        </CardContent>
      </Card>
    );
  }
  return (
    <section aria-label='Precisa de você' className='space-y-3'>
      <h2 className='text-lg font-semibold'>Precisa de você</h2>
      <div className='grid gap-4 lg:grid-cols-2'>
        {shown.map((s) => (
          <BlockCard
            key={s.key}
            spec={s}
            block={overview.blocks[s.key]}
            overview={overview}
            now={now}
            escopo={escopo}
          />
        ))}
      </div>
    </section>
  );
}
