'use client';

import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { QueryError } from '@/components/query-error';
import { Label } from '@/components/ui/label';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { accessOf } from '../../access';
import { checklistQueryOptions } from '../../api/queries';
import type { Project } from '../../api/types';
import { ChecklistItemCard } from '../checklist-item';
import { ExtraDocuments } from '../extra-documents';

/**
 * Documentos do projeto em leitura: situação dos obrigatórios, o checklist agrupado e os documentos adicionais. Cada arquivo abre no
 * visualizador do sistema (a equipe que confere decide ali) ou pode ser baixado.
 */
export function ProjectDocuments({ project }: { project: Project }) {
  const checklist = useQuery(checklistQueryOptions(project.id));
  const [onlyMissing, setOnlyMissing] = useState(false);

  if (checklist.isError)
    return <QueryError error={checklist.error} what='o checklist de documentos' />;
  if (checklist.isLoading || !checklist.data) return <Skeleton className='h-60 w-full' />;

  const { summary, items, instrument, extraDocuments } = checklist.data;
  if (!instrument)
    return <p className='text-muted-foreground text-sm'>O instrumento ainda não foi escolhido.</p>;

  // A equipe nunca edita por aqui: o acesso de quem não é titular nem membro nega tudo.
  const access = accessOf(project, false, 0);
  const optional = items.filter((i) => !i.mandatory);
  const requiredDone = summary.requiredTotal - summary.requiredMissing;
  const requiredJustified = items.filter((i) => i.mandatory && i.status === 'justified').length;
  const optionalSent = optional.filter((i) => i.status !== 'missing').length;
  const visible = onlyMissing ? items.filter((i) => i.status === 'missing') : items;
  const groups = Array.from(new Set(visible.map((i) => i.group)));

  return (
    <div className='space-y-6'>
      <div className='space-y-2'>
        <div className='flex flex-wrap items-baseline justify-between gap-2'>
          <p className='text-sm font-medium'>
            {requiredDone} de {summary.requiredTotal} documentos obrigatórios cumpridos
            {requiredJustified > 0 && (
              <span className='text-muted-foreground font-normal'>
                {' '}
                ({requiredJustified} com justificativa de não envio)
              </span>
            )}
          </p>
          <p className='text-muted-foreground text-sm'>
            {summary.requiredMissing} obrigatórios pendentes
          </p>
        </div>
        <Progress
          value={
            summary.requiredTotal ? Math.round((requiredDone / summary.requiredTotal) * 100) : 100
          }
          aria-label='Documentos obrigatórios cumpridos'
        />
        {optional.length > 0 && (
          <p className='text-muted-foreground text-sm'>
            {optionalSent} de {optional.length} documentos opcionais enviados.
          </p>
        )}
      </div>

      <div className='flex items-center gap-2'>
        <Switch id='view-only-missing' checked={onlyMissing} onCheckedChange={setOnlyMissing} />
        <Label htmlFor='view-only-missing' className='text-sm'>
          Mostrar só o que falta
        </Label>
      </div>
      {visible.length === 0 && (
        <p className='text-muted-foreground text-sm'>Nada pendente nesta lista.</p>
      )}
      {groups.map((g) => (
        <section key={g} aria-labelledby={`view-group-${g}`} className='space-y-3'>
          <h4 id={`view-group-${g}`} className='text-base font-semibold'>
            {g}
          </h4>
          <ul className='space-y-3'>
            {visible
              .filter((i) => i.group === g)
              .map((item) => (
                <ChecklistItemCard
                  key={`${item.code}-${item.amendmentId}-${item.justification}`}
                  project={project}
                  item={item}
                  editable={false}
                />
              ))}
          </ul>
        </section>
      ))}
      {!onlyMissing && extraDocuments.length > 0 && (
        <ExtraDocuments project={project} docs={extraDocuments} access={access} readOnly />
      )}
    </div>
  );
}
