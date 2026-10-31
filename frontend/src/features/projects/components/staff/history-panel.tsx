'use client';

import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { Project } from '../../api/types';
import { AuditHistory } from '@/features/audit-logs/components/audit-history';
import { formatDateTimeCuiaba } from '../../format';
import { StageTimeline } from './stage-timeline';

/** Histórico do projeto para a equipe: as etapas, o pedido de complementação mais recente e, para quem pode, o registro de auditoria. */
export function HistoryPanel({ project }: { project: Project }) {
  const c = project.complement;
  return (
    <div className='max-w-3xl space-y-4'>
      {project.status !== 'draft' && <StageTimeline projectId={project.id} />}
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>Pedido de complementação</CardTitle>
        </CardHeader>
        <CardContent className='space-y-2 text-sm'>
          {!c ? (
            <p className='text-muted-foreground'>Nenhuma complementação foi pedida.</p>
          ) : (
            <>
              <p>
                Pedido nº {c.number} em {formatDateTimeCuiaba(c.requestedAt)}, com prazo até{' '}
                {formatDateTimeCuiaba(c.dueAt)}.{' '}
                <Badge variant='outline'>
                  {
                    {
                      open: 'Aberto',
                      answered: 'Respondido',
                      expired: 'Vencido',
                      cancelled: 'Cancelado'
                    }[c.status]
                  }
                </Badge>
              </p>
              {c.message && <p className='whitespace-pre-line'>{c.message}</p>}
              <ul className='list-disc space-y-1 pl-5'>
                {c.items.map((it) => (
                  <li key={`${it.kind}-${it.target}`}>
                    <span className='font-medium'>{it.label}</span>
                    {it.note && <span className='text-muted-foreground'>: {it.note}</span>}
                  </li>
                ))}
              </ul>
            </>
          )}
        </CardContent>
      </Card>
      <AuditHistory entity='project' entityId={project.id} />
    </div>
  );
}
