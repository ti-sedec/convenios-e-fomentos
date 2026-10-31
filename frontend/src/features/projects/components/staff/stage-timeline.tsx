'use client';

import { useQuery } from '@tanstack/react-query';
import { QueryError } from '@/components/query-error';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { stageHistoryQueryOptions } from '../../api/queries';
import {
  historyHolderLabels,
  stageLabels,
  type HistorySegment,
  type StageHistory
} from '../../api/types';
import { formatDateTimeCuiaba, formatDuration } from '../../format';

/** O que abriu cada trecho, com o mesmo código do registro de auditoria. */
const transitionLabels: Record<string, string> = {
  initial: 'Estado quando o histórico começou',
  'project.submit': 'Projeto enviado',
  'project.start-analysis': 'Análise assumida',
  'project.complement-request': 'Devolvido para complementação',
  'project.resubmit': 'Reenviado pelo proponente',
  'project.assign-department': 'Departamento indicado',
  'project.framing-confirm': 'Enquadramento confirmado',
  'project.framing-refuse': 'Enquadramento recusado',
  'project.archive': 'Arquivado'
};

function segmentTitle(s: HistorySegment) {
  switch (s.holder) {
    case 'analyst':
      return s.analyst?.name ? `Analista ${s.analyst.name}` : 'Analista';
    case 'department':
      return s.department?.name ? `Departamento ${s.department.name}` : 'Departamento';
    case 'team':
      return s.stage === 'awaiting_checklist' ? 'Equipe, aguardando a conferência' : 'Equipe';
    default:
      return historyHolderLabels[s.holder];
  }
}

function enteredByText(s: HistorySegment) {
  if (!s.enteredBy || s.estimated) return '';
  if (s.enteredBy.type === 'system') return 'pelo sistema';
  return s.enteredBy.name ? `por ${s.enteredBy.name}` : '';
}

function Totals({ summary }: { summary: StageHistory['summary'] }) {
  const team = (summary.byHolder.team ?? 0) + (summary.byHolder.analyst ?? 0);
  const items = [
    { label: 'Desde o envio', value: summary.sinceSubmissionSeconds },
    { label: 'Na etapa atual', value: summary.currentStageSeconds },
    { label: 'Com a equipe', value: team },
    { label: 'Com o proponente', value: summary.byHolder.proponent ?? 0 },
    { label: 'Com o departamento', value: summary.byHolder.department ?? 0 }
  ];
  return (
    <dl className='grid grid-cols-2 gap-3 sm:grid-cols-5'>
      {items.map((i) => (
        <div key={i.label} className='bg-muted/40 rounded-md px-3 py-2'>
          <dt className='text-muted-foreground text-xs'>{i.label}</dt>
          <dd className='text-sm font-medium'>{formatDuration(i.value)}</dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * Por onde o projeto passou e por quanto tempo, com quem estava em cada momento. Horas corridas: a contagem não desconta fins de
 * semana nem feriados, e o tempo para ao arquivar.
 */
export function StageTimeline({ projectId }: { projectId: string }) {
  const history = useQuery(stageHistoryQueryOptions(projectId));
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>Etapas do projeto</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4'>
        {history.isLoading ? (
          <Skeleton className='h-40 w-full' />
        ) : history.isError ? (
          <QueryError error={history.error} what='as etapas do projeto' />
        ) : !history.data || history.data.segments.length === 0 ? (
          <p className='text-muted-foreground text-sm'>O projeto ainda não foi enviado.</p>
        ) : (
          <>
            <Totals summary={history.data.summary} />
            <ol className='border-border relative ml-2 space-y-5 border-l pl-6'>
              {history.data.segments.toReversed().map((s) => {
                const current = s.leftAt === null;
                return (
                  <li key={s.seq} className='relative'>
                    <span
                      aria-hidden
                      className={`absolute top-1.5 -left-[1.9rem] size-3 rounded-full border-2 ${
                        current
                          ? 'border-primary bg-primary'
                          : 'border-muted-foreground bg-background'
                      }`}
                    />
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='text-sm font-medium'>{segmentTitle(s)}</span>
                      {current && s.holder !== 'none' && <Badge>Agora</Badge>}
                      {s.estimated && (
                        <Badge
                          variant='outline'
                          title='Projeto anterior ao histórico: horário aproximado'
                        >
                          Estimado
                        </Badge>
                      )}
                    </div>
                    <p className='text-muted-foreground text-sm'>
                      {s.holder === 'none' ? 'Arquivado' : stageLabels[s.stage]}
                      {s.holder !== 'none' && ` · ${formatDuration(s.durationSeconds)}`}
                    </p>
                    <p className='text-muted-foreground text-xs'>
                      {formatDateTimeCuiaba(s.enteredAt)}
                      {s.leftAt && ` até ${formatDateTimeCuiaba(s.leftAt)}`} ·{' '}
                      {transitionLabels[s.transition] ?? s.transition} {enteredByText(s)}
                    </p>
                  </li>
                );
              })}
            </ol>
            <p className='text-muted-foreground text-xs'>
              Horas corridas: a contagem inclui fins de semana e feriados.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}
