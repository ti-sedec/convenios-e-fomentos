import Link from 'next/link';
import { Card, CardContent } from '@/components/ui/card';
import type { QueueCounts } from '../api/types';
import type { ScopeKey } from './scope-filter';

/** Os cartões levam à aba da fila (`?aba=`); em "só os meus", também ao filtro dos que a pessoa assumiu (`?analista=eu`). */
const cards: { tab: string; label: string; count: (c: QueueCounts) => number; entry?: boolean }[] =
  [
    {
      tab: 'aguardando',
      label: 'Aguardando análise',
      count: (c) => c.awaiting_checklist,
      entry: true
    },
    { tab: 'analise', label: 'Em análise', count: (c) => c.in_analysis },
    { tab: 'enquadramento', label: 'Em enquadramento', count: (c) => c.framing_pending },
    { tab: 'proponente', label: 'Aguardam proponente', count: (c) => c.awaiting_proponent },
    { tab: 'parecer', label: 'Aguardando parecer', count: (c) => c.awaiting_opinion },
    { tab: 'arquivados', label: 'Arquivados (30 dias)', count: (c) => c.archived_30d }
  ];

export const queueHref = (tab: string, escopo: ScopeKey) =>
  `/dashboard/analise?aba=${tab}${escopo === 'meus' ? '&analista=eu' : ''}`;

/** Um cartão por etapa da fila. Em "só os meus", "aguardando análise" fica fora: ninguém é dono antes de assumir. */
export function QueueCounters({ counts, escopo }: { counts: QueueCounts; escopo: ScopeKey }) {
  const shown = cards.filter((c) => !(escopo === 'meus' && c.entry));
  return (
    <section aria-label='Fila por etapa'>
      <ul className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6'>
        {shown.map((c) => (
          <li key={c.tab}>
            <Link
              href={queueHref(c.tab, escopo)}
              aria-label={`${c.label}: ${c.count(counts)}. Ver a fila`}
              className='hover:bg-muted focus-visible:ring-ring/50 block rounded-xl focus-visible:ring-[3px] focus-visible:outline-none'
            >
              <Card size='sm' className='h-full'>
                <CardContent>
                  <p className='text-3xl font-semibold tabular-nums'>{c.count(counts)}</p>
                  <p className='text-muted-foreground text-sm'>{c.label}</p>
                </CardContent>
              </Card>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
