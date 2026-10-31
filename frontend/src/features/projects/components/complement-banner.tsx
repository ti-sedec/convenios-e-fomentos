'use client';

import { Icons } from '@/components/icons';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { focusIdOf, stepOfItem } from '../access';
import {
  archiveReasonLabels,
  type ComplementItem,
  type Project,
  type SectionKey
} from '../api/types';

const dateTime = new Intl.DateTimeFormat('pt-BR', {
  dateStyle: 'short',
  timeStyle: 'short',
  timeZone: 'America/Cuiaba'
});

/** Quanto falta para o prazo, em linguagem simples. */
function remaining(due: string, now: number): string {
  const ms = new Date(due).getTime() - now;
  if (ms <= 0) return 'prazo vencido';
  const hours = Math.floor(ms / 3_600_000);
  if (hours < 1) return 'menos de 1 hora';
  if (hours < 48) return `${hours} hora${hours === 1 ? '' : 's'}`;
  return `${Math.ceil(hours / 24)} dias`;
}

function Item({
  item,
  onGo
}: {
  item: ComplementItem;
  onGo: (s: SectionKey, focus?: string) => void;
}) {
  return (
    <li className='flex gap-2'>
      {item.answered ? (
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
        <button
          type='button'
          className='font-medium underline underline-offset-4'
          onClick={() => onGo(stepOfItem(item), focusIdOf(item))}
        >
          {item.label}
        </button>
        {item.note && <span className='text-muted-foreground'>: {item.note}</span>}
      </span>
    </li>
  );
}

/** Faixa de situação do projeto depois do envio: correções pedidas pela equipe, ou arquivamento. */
export function LifecycleBanner({
  project,
  now,
  onGo
}: {
  project: Project;
  now: number;
  onGo: (s: SectionKey, focus?: string) => void;
}) {
  const c = project.complement;

  if (project.status === 'archived' && project.archive) {
    return (
      <Alert variant='destructive'>
        <AlertTitle>Projeto arquivado: {archiveReasonLabels[project.archive.reason]}</AlertTitle>
        <AlertDescription>
          <p>Arquivado em {dateTime.format(new Date(project.archive.at))}.</p>
          {project.archive.note && (
            <p className='mt-1 whitespace-pre-line'>{project.archive.note}</p>
          )}
          <p className='mt-2'>
            Projeto arquivado não volta a andar. Para tentar de novo, use{' '}
            <strong>Duplicar projeto</strong>: o novo rascunho começa com os mesmos dados.
          </p>
        </AlertDescription>
      </Alert>
    );
  }

  if (
    project.status === 'submitted' &&
    project.stage === 'awaiting_proponent' &&
    c?.status === 'open'
  ) {
    const overdue = c.overdue || new Date(c.dueAt).getTime() <= now;
    return (
      <Alert variant={overdue ? 'destructive' : 'default'}>
        <Icons.alertCircle aria-hidden />
        <AlertTitle>
          {overdue ? 'O prazo para as correções venceu' : 'A equipe da SEDEC pediu correções'}
        </AlertTitle>
        <AlertDescription>
          <p>
            Prazo: <strong>{dateTime.format(new Date(c.dueAt))}</strong> (horário de Cuiabá) ·{' '}
            {remaining(c.dueAt, now)}.{' '}
            {overdue
              ? 'O projeto será arquivado.'
              : 'Se o prazo vencer sem o reenvio, o projeto é arquivado e será preciso começar outro.'}
          </p>
          {c.message && <p className='mt-2 whitespace-pre-line'>{c.message}</p>}
          <p className='mt-3 text-sm font-medium'>Itens devolvidos</p>
          <ul className='mt-1 space-y-1'>
            {c.items.map((it) => (
              <Item key={`${it.kind}-${it.target}`} item={it} onGo={onGo} />
            ))}
          </ul>
          <p className='mt-3'>
            Corrija os itens e, na última etapa, use <strong>Reenviar com as correções</strong>.
          </p>
        </AlertDescription>
      </Alert>
    );
  }

  if (project.status === 'submitted' && project.stage === 'awaiting_opinion') {
    return (
      <Alert>
        <Icons.circleCheck aria-hidden />
        <AlertTitle>Projeto enquadrado</AlertTitle>
        <AlertDescription>O projeto foi enquadrado e aguarda o parecer da equipe.</AlertDescription>
      </Alert>
    );
  }

  if (
    project.status === 'submitted' &&
    project.stage === 'in_analysis' &&
    c?.status === 'answered'
  ) {
    return (
      <Alert>
        <Icons.circleCheck aria-hidden />
        <AlertTitle>Correções enviadas</AlertTitle>
        <AlertDescription>O projeto voltou para a análise da equipe da SEDEC.</AlertDescription>
      </Alert>
    );
  }
  return null;
}
