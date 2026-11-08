import Link from 'next/link';
import { Card, CardContent } from '@/components/ui/card';
import type { ReviewerCounts } from '../api/types';

/** Cada cartão leva à aba da fila (`?aba=`) do Revisor. */
const cards: { tab: string; label: string; count: (c: ReviewerCounts) => number }[] = [
  { tab: 'pendentes', label: 'Pendentes de decisão', count: (c) => c.framing_pending },
  {
    tab: 'confirmados',
    label: 'Enquadrados, aguardando parecer',
    count: (c) => c.awaiting_opinion
  },
  { tab: 'arquivados', label: 'Arquivados (30 dias)', count: (c) => c.archived_30d }
];

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** Os números do departamento, mais as decisões dos últimos 30 dias (de qualquer Revisor dele). */
export function DepartmentCounters({ counts }: { counts: ReviewerCounts }) {
  const { confirmed, refused } = counts.decided_30d;
  return (
    <section aria-label='Departamento' className='space-y-2'>
      <ul className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        {cards.map((c) => (
          <li key={c.tab}>
            <Link
              href={`/dashboard/analise?aba=${c.tab}`}
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
      <p className='text-muted-foreground text-sm'>
        Decididos pelo departamento nos últimos 30 dias:{' '}
        {plural(confirmed, 'confirmado', 'confirmados')} e{' '}
        {plural(refused, 'recusado', 'recusados')}.
      </p>
    </section>
  );
}
