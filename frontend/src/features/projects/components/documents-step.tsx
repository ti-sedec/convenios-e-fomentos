'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { LoadingButton } from '@/components/ui/loading-button';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';
import { Switch } from '@/components/ui/switch';
import { QueryError } from '@/components/query-error';
import { FieldGroup } from '@/components/ui/field';
import { useAppForm } from '@/lib/form';
import { checklistQueryOptions, invalidateAfterSave, projectKeys } from '../api/queries';
import { saveDocumentOptions } from '../api/service';
import type { Access } from '../access';
import type { ChecklistItem, Project, SectionKey } from '../api/types';
import { ChecklistItemCard, itemDomId } from './checklist-item';
import { ExtraDocuments } from './extra-documents';
import { ServerErrors } from './server-errors';

/** Situações informadas pelo proponente que acrescentam documentos ao checklist. */
function OptionsForm({ project, readOnly }: { project: Project; readOnly: boolean }) {
  const qc = useQueryClient();
  const o = project.documentOptions;
  const fomento = project.institution.instrument === 'fomento';
  const save = useMutation({
    mutationFn: (v: typeof o) =>
      saveDocumentOptions(project.id, { version: project.version, ...v }),
    onSuccess: async (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      await invalidateAfterSave(qc, project.id);
      toast.success('Situações salvas. O checklist foi atualizado.');
    }
  });
  const form = useAppForm({
    defaultValues: { ...o },
    onSubmit: ({ value }) => save.mutateAsync(value).catch(() => undefined)
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>Situações que pedem documentos adicionais</CardTitle>
          <CardDescription>
            Marque o que se aplica ao projeto; o checklist abaixo se ajusta.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <form.AppField
              name='exclusiveSupplier'
              children={(f) => (
                <f.SwitchField
                  label='Contratação por exclusividade'
                  description='Fornecedor ou artista exclusivo: pede a documentação de exclusividade.'
                />
              )}
            />
            {fomento && (
              <form.AppField
                name='publicSpaceUse'
                children={(f) => (
                  <f.SwitchField
                    label='Atividade em espaço público'
                    description='Pede a autorização de uso do espaço.'
                  />
                )}
              />
            )}
            <form.AppField
              name='reducedDeadline'
              children={(f) => (
                <f.SwitchField
                  label='Pedido de redução do prazo de tramitação'
                  description='Pede a justificativa (modelo da SEDEC).'
                />
              )}
            />
            <ServerErrors error={save.error} />
            <div>
              <LoadingButton type='submit' size='sm' loading={save.isPending} disabled={readOnly}>
                Salvar situações
              </LoadingButton>
            </div>
          </FieldGroup>
        </CardContent>
      </Card>
    </form>
  );
}

/**
 * `onContinue` é do proponente (leva à revisão e envio). A equipe, que só lê, não o passa: some a barra de "continuar" e o texto sobre
 * o que se aceita anexar.
 */
export function DocumentsStep({
  project,
  access,
  focus,
  onContinue,
  onGoStep
}: {
  project: Project;
  access: Access;
  /** Leva a pessoa a outra etapa do cadastro (as pendências das declarações apontam para onde corrigir). */
  onGoStep?: (step: SectionKey) => void;
  /** Id do elemento a levar para o centro da tela quando a lista aparecer (vindo do aviso de correções). */
  focus?: string;
  onContinue?: () => void;
}) {
  const checklist = useQuery(checklistQueryOptions(project.id));
  const loaded = !!checklist.data;
  useEffect(() => {
    if (focus && loaded) document.getElementById(focus)?.scrollIntoView({ block: 'center' });
  }, [focus, loaded]);
  // Com um pedido de complementação aberto, a lista já abre só no que falta ou foi devolvido.
  const [onlyMissing, setOnlyMissing] = useState(access.complement !== null);

  if (checklist.isError)
    return <QueryError error={checklist.error} what='o checklist de documentos' />;
  if (checklist.isLoading || !checklist.data) return <Skeleton className='h-80 w-full' />;
  const { summary, items, instrument } = checklist.data;
  const required = items.filter((i) => i.mandatory);
  const optional = items.filter((i) => !i.mandatory);
  const requiredDone = summary.requiredTotal - summary.requiredMissing;
  const requiredJustified = required.filter((i) => i.status === 'justified').length;
  const optionalSent = optional.filter((i) => i.status !== 'missing').length;
  const blockers = required.filter((i) => i.status === 'missing');
  // O que ainda pede ação: pendente (menos a declaração que só espera a assinatura) ou devolvido pela equipe, mesmo com arquivo.
  const needsAction = (i: ChecklistItem) =>
    (i.status === 'missing' && !i.declarationAwaiting) || !!access.returnedDocument(i);
  const visible = onlyMissing ? items.filter(needsAction) : items;
  const groups = Array.from(new Set(visible.map((i) => i.group)));
  const byGroup = (g: string): ChecklistItem[] => visible.filter((i) => i.group === g);

  return (
    <div className='space-y-6'>
      {!instrument ? (
        <Alert>
          <AlertDescription>
            Escolha o instrumento na etapa Instituição para ver o checklist de documentos.
          </AlertDescription>
        </Alert>
      ) : (
        <>
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
                summary.requiredTotal
                  ? Math.round((requiredDone / summary.requiredTotal) * 100)
                  : 100
              }
              aria-label='Documentos obrigatórios cumpridos'
            />
            {optional.length > 0 && (
              <p className='text-muted-foreground text-sm'>
                {optionalSent} de {optional.length} documentos opcionais enviados. Os opcionais não
                impedem de continuar.
              </p>
            )}
            {onContinue && (
              <p className='text-muted-foreground text-xs'>
                Aceitamos PDF, DOCX, PNG, JPEG e WebP, com até 10 arquivos por documento. Os modelos
                são os publicados pela SEDEC.
              </p>
            )}
          </div>
          <OptionsForm
            key={project.version}
            project={project}
            readOnly={
              !(
                access.draft ||
                access.complement?.items.some((i) => i.target === 'document-options')
              )
            }
          />
          <div className='flex items-center gap-2'>
            <Switch id='only-missing' checked={onlyMissing} onCheckedChange={setOnlyMissing} />
            <Label htmlFor='only-missing' className='text-sm'>
              {access.complement
                ? 'Mostrar só o que falta ou foi devolvido'
                : 'Mostrar só o que falta'}
            </Label>
          </div>
          {visible.length === 0 && (
            <p className='text-muted-foreground text-sm'>Nada pendente nesta lista.</p>
          )}
          {groups.map((g) => (
            <section key={g} aria-labelledby={`group-${g}`} className='space-y-3'>
              <h3 id={`group-${g}`} className='text-base font-semibold'>
                {g}
              </h3>
              <ul className='space-y-3'>
                {byGroup(g).map((item) => (
                  <ChecklistItemCard
                    key={`${item.code}-${item.amendmentId}-${item.justification}`}
                    project={project}
                    item={item}
                    editable={access.document(item)}
                    returned={access.returnedDocument(item)}
                    onGoStep={onGoStep}
                  />
                ))}
              </ul>
            </section>
          ))}
          <ExtraDocuments
            project={project}
            docs={checklist.data.extraDocuments}
            access={access}
            onlyReturned={onlyMissing && access.complement !== null}
          />
          {onContinue && <ContinueBar blockers={blockers} onContinue={onContinue} />}
        </>
      )}
    </div>
  );
}

/** Rodapé da aba: só libera a revisão quando não falta documento obrigatório, e diz o que falta quando não libera. */
function ContinueBar({
  blockers,
  onContinue
}: {
  blockers: ChecklistItem[];
  onContinue: () => void;
}) {
  const blocked = blockers.length > 0;
  const goTo = (item: ChecklistItem) =>
    document.getElementById(itemDomId(item))?.scrollIntoView({ block: 'center' });

  return (
    <div className='space-y-3 border-t pt-4'>
      {blocked && (
        <div id='documents-blockers' className='space-y-1 text-sm'>
          <p className='font-medium'>
            Faltam {blockers.length} documentos obrigatórios. Anexe o arquivo ou justifique o não
            envio:
          </p>
          <ul className='list-disc space-y-0.5 pl-5'>
            {blockers.map((item) => (
              <li key={`${item.code}-${item.amendmentId}`}>
                <button
                  type='button'
                  className='text-left underline underline-offset-4'
                  onClick={() => goTo(item)}
                >
                  {item.name}
                  {item.amendmentLabel ? ` (${item.amendmentLabel})` : ''}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <Button
        type='button'
        disabled={blocked}
        aria-describedby={blocked ? 'documents-blockers' : undefined}
        onClick={onContinue}
      >
        Continuar para Revisão e envio
      </Button>
    </div>
  );
}
