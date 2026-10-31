import Link from 'next/link';
import { Card, CardContent } from '@/components/ui/card';
import type { AccessKey } from '@/features/projects/access-filter';
import { formatBrl } from '@/features/projects/format';
import { situations } from '@/features/projects/situations';
import { projectsHref } from '../format';
import type { Overview } from '../api/types';

/** Um cartão por situação (cada projeto conta em um só), que leva à lista já filtrada, e o valor em tramitação. */
export function StatusCounters({ overview, acesso }: { overview: Overview; acesso: AccessKey }) {
  const { counts, activeCents } = overview;
  return (
    <section aria-label='Projetos por situação' className='space-y-3'>
      <ul className='grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-5'>
        {situations.map((s) => (
          <li key={s.key}>
            <Link
              href={projectsHref(s.key, acesso)}
              aria-label={`${s.label}: ${s.count(counts)}. Ver os projetos`}
              className='hover:bg-muted focus-visible:ring-ring/50 block rounded-xl focus-visible:ring-[3px] focus-visible:outline-none'
            >
              <Card size='sm' className='h-full'>
                <CardContent>
                  <p className='text-3xl font-semibold tabular-nums'>{s.count(counts)}</p>
                  <p className='text-muted-foreground text-sm'>{s.label}</p>
                </CardContent>
              </Card>
            </Link>
          </li>
        ))}
      </ul>
      <p className='text-muted-foreground text-sm'>
        Em tramitação (enviados, ainda ativos):{' '}
        <span className='text-foreground font-medium tabular-nums'>
          {formatBrl(activeCents.total)}
        </span>
        {activeCents.convenio > 0 && activeCents.fomento > 0 && (
          <>
            {' '}
            · Convênios {formatBrl(activeCents.convenio)} · Termos de fomento{' '}
            {formatBrl(activeCents.fomento)}
          </>
        )}
      </p>
    </section>
  );
}
