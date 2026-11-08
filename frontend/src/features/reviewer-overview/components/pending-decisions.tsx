'use client';

import Link from 'next/link';
import { useState } from 'react';
import { Badge } from '@/components/ui/badge';
import { Button, buttonVariants } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { projectHref } from '@/features/proponent-overview/format';
import { formatDateTimeCuiaba, formatDuration } from '@/features/projects/format';
import type { ReviewerOverview } from '../api/types';

const SHOWN = 5;

/** Aguardando a sua decisão: os enquadramentos do departamento, do que está há mais tempo com ele para o mais novo. */
export function PendingDecisions({ overview }: { overview: ReviewerOverview }) {
  const [all, setAll] = useState(false);
  const { total, items } = overview.pending;
  const shown = all ? items : items.slice(0, SHOWN);
  return (
    <section aria-label='Aguardando a sua decisão'>
      <Card>
        <CardHeader>
          <CardTitle className='flex flex-wrap items-center gap-2'>
            Aguardando a sua decisão
            <Badge variant='secondary' className='tabular-nums'>
              {total}
            </Badge>
          </CardTitle>
          <p className='text-muted-foreground text-xs'>
            Vale a primeira decisão de qualquer Revisor do departamento: um projeto pode sair desta
            lista se um colega decidir antes.
          </p>
        </CardHeader>
        <CardContent>
          {total === 0 ? (
            <p className='text-muted-foreground text-sm'>
              Nenhum enquadramento aguarda a sua decisão.
            </p>
          ) : (
            <>
              <ul className='divide-y'>
                {shown.map((it) => (
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
                        {[it.code, it.institution].filter(Boolean).join(' · ')}
                      </p>
                      <p className='text-muted-foreground text-xs'>
                        {`Indicado${it.indicatedBy ? ` por ${it.indicatedBy}` : ''} em ${formatDateTimeCuiaba(it.indicatedAt)}`}
                        {it.round > 1 ? ` (indicação nº ${it.round})` : ''}
                      </p>
                      <p
                        className={`text-xs ${it.overLimit ? 'text-destructive font-medium' : 'text-muted-foreground'}`}
                      >
                        {`Com o departamento há ${formatDuration(it.sinceSeconds)}`}
                        {it.overLimit ? `, acima do limite de ${overview.limits.framing} dias` : ''}
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
                      Analisar
                    </Link>
                  </li>
                ))}
              </ul>
              <div className='mt-2 flex flex-wrap items-center gap-2'>
                {items.length > SHOWN && (
                  <Button
                    size='sm'
                    variant='ghost'
                    aria-expanded={all}
                    onClick={() => setAll((v) => !v)}
                  >
                    {all ? 'Mostrar menos' : `Mostrar mais (${items.length - SHOWN})`}
                  </Button>
                )}
                <Link
                  href='/dashboard/analise?aba=pendentes'
                  className={buttonVariants({ size: 'sm', variant: 'ghost' })}
                >
                  {total > items.length ? `Ver todos os ${total} na fila` : 'Ver na fila'}
                </Link>
              </div>
            </>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
