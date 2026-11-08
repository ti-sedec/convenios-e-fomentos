import type { Amendment, DocumentSummary, FundingSection, ProposalSection } from '../../api/types';
import { formatBrl, formatDate } from '../../format';
import { ViewRow } from './primitives';

/** Vigência (quando há mais de um período) e os períodos com os seus locais. */
export function ExecutionRows({ proposal: p }: { proposal: ProposalSection }) {
  const many = p.executionPeriods.length > 1;
  return (
    <>
      <ViewRow label='Vigência'>
        {many && p.startDate && p.endDate
          ? `${formatDate(p.startDate)} a ${formatDate(p.endDate)}`
          : ''}
      </ViewRow>
      <ViewRow label={many ? 'Períodos e locais' : 'Execução em'}>
        {p.executionPeriods.length > 0 && (
          <ul className='space-y-1'>
            {p.executionPeriods.map((e, i) => (
              <li key={e.id ?? i}>
                {[
                  e.startDate && e.endDate
                    ? `${formatDate(e.startDate)} a ${formatDate(e.endDate)}`
                    : '',
                  e.municipality?.name ?? '',
                  e.placeDetail
                ]
                  .filter(Boolean)
                  .join(' — ')}
              </li>
            ))}
          </ul>
        )}
      </ViewRow>
    </>
  );
}

function amendmentRef(a: Amendment) {
  return [a.number && `nº ${a.number}`, a.year > 0 && a.year].filter(Boolean).join('/');
}

/** Uma linha por emenda: parlamentar, número/ano e valor. */
export function AmendmentRows({ funding: f }: { funding: FundingSection }) {
  return (
    <>
      {f.amendments.map((a, n) => (
        <ViewRow key={a.id || n} label={`Emenda ${n + 1}`}>
          <div className='flex flex-wrap items-baseline justify-between gap-x-4'>
            <span>
              {a.parliamentarianName || 'Parlamentar não informado'}
              {(a.number || a.year > 0) && (
                <span className='text-muted-foreground'>
                  {' · '}
                  {amendmentRef(a)}
                </span>
              )}
            </span>
            <span className='tabular-nums'>{formatBrl(a.amountCents)}</span>
          </div>
        </ViewRow>
      ))}
    </>
  );
}

/** "3 anexados, 1 justificados, 2 pendentes (de 6)"; vazio quando o checklist ainda não existe. */
export function documentsLine(d: DocumentSummary) {
  return d.total
    ? `${d.done} anexados, ${d.justified} justificados, ${d.missing} pendentes (de ${d.total})`
    : '';
}
