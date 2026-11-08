'use client';

import { Icons } from '@/components/icons';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { useCan } from '@/features/auth/session-provider';
import { archiveReasonLabels, projectActions, type Project } from '../../api/types';
import { formatDateTimeCuiaba } from '../../format';

/**
 * Aviso ao Analista que não é o responsável: vê o projeto, mas não continua a análise sem assumi-lo. Só aparece enquanto a análise
 * está com a equipe (em análise ou aguardando o proponente).
 */
export function ResponsibleNotice({ project }: { project: Project }) {
  const can = useCan();
  const analyst = project.analyst;
  if (!can(projectActions.startAnalysis) || !analyst || analyst.isMe) return null;
  if (project.status !== 'submitted') return null;
  if (project.stage !== 'in_analysis' && project.stage !== 'awaiting_proponent') return null;
  return (
    <Alert>
      <Icons.eye aria-hidden />
      <AlertTitle>Este projeto está com {analyst.name}</AlertTitle>
      <AlertDescription>
        Você pode visualizá-lo, mas não pode conferir itens, devolver, arquivar nem indicar o
        departamento. Para continuar a análise, assuma o projeto: ele passa a ser seu.
      </AlertDescription>
    </Alert>
  );
}

/** Faixa de situação do projeto para a equipe: arquivamento, correções com o proponente, decisão do departamento ou parecer. */
export function StaffBanner({ project }: { project: Project }) {
  const c = project.complement;

  if (project.status === 'archived' && project.archive) {
    return (
      <Alert variant='destructive'>
        <Icons.alertCircle aria-hidden />
        <AlertTitle>Projeto arquivado: {archiveReasonLabels[project.archive.reason]}</AlertTitle>
        <AlertDescription>
          <p>
            Arquivado em {formatDateTimeCuiaba(project.archive.at)}. O projeto é somente leitura.
          </p>
          {project.archive.note && (
            <p className='mt-1 whitespace-pre-line'>{project.archive.note}</p>
          )}
        </AlertDescription>
      </Alert>
    );
  }
  if (project.status !== 'submitted') return null;

  if (project.stage === 'awaiting_proponent' && c?.status === 'open') {
    return (
      <Alert>
        <Icons.alertCircle aria-hidden />
        <AlertTitle>Aguardando as correções do proponente</AlertTitle>
        <AlertDescription>
          <p>
            Prazo: <strong>{formatDateTimeCuiaba(c.dueAt)}</strong> (horário de Cuiabá). Se vencer
            sem o reenvio, o projeto é arquivado.
          </p>
          {c.message && <p className='mt-2 whitespace-pre-line'>{c.message}</p>}
          <p className='mt-3 text-sm font-medium'>Itens devolvidos</p>
          <ul className='mt-1 space-y-1'>
            {c.items.map((it) => (
              <li key={`${it.kind}-${it.target}`} className='flex gap-2'>
                {it.answered ? (
                  <Icons.circleCheck
                    className='mt-0.5 size-4 shrink-0 text-green-600'
                    aria-label='Já alterado'
                  />
                ) : (
                  <Icons.circle
                    className='text-muted-foreground mt-0.5 size-4 shrink-0'
                    aria-label='Ainda não alterado'
                  />
                )}
                <span>
                  <span className='font-medium'>{it.label}</span>
                  {it.note && <span className='text-muted-foreground'>: {it.note}</span>}
                </span>
              </li>
            ))}
          </ul>
        </AlertDescription>
      </Alert>
    );
  }

  if (project.framing?.decision === 'pending') {
    return (
      <Alert>
        <Icons.alertCircle aria-hidden />
        <AlertTitle>Aguardando a decisão do departamento</AlertTitle>
        <AlertDescription>
          O departamento <strong>{project.framing.department.name}</strong> foi indicado em{' '}
          {formatDateTimeCuiaba(project.framing.indicatedAt)} (indicação nº {project.framing.round})
          e o Revisor do Departamento ainda não decidiu.
        </AlertDescription>
      </Alert>
    );
  }

  if (project.stage === 'awaiting_opinion') {
    return (
      <Alert>
        <Icons.circleCheck aria-hidden />
        <AlertTitle>Projeto enquadrado: aguardando parecer</AlertTitle>
        <AlertDescription>
          Enquadramento confirmado
          {project.framing ? ` pelo departamento ${project.framing.department.name}` : ''}.
        </AlertDescription>
      </Alert>
    );
  }
  return null;
}
