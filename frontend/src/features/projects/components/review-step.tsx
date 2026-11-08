'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { AlertModal } from '@/components/modal/alert-modal';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { LoadingButton } from '@/components/ui/loading-button';
import { ApiError } from '@/lib/api/errors';
import { errorMessage } from '@/lib/errors';
import { projectKeys } from '../api/queries';
import { downloadReceipt, resubmitProject, submitProject } from '../api/service';
import { focusIdOf, stepOfItem, type Access } from '../access';
import {
  fundingTypeLabels,
  instrumentLabels,
  sectionLabels,
  stageLabels,
  type Project,
  type SectionKey
} from '../api/types';
import { formatBrl, formatCnpj } from '../format';
import { AmendmentRows, ExecutionRows, documentsLine } from './project-view/parts';
import { ViewRow as Row } from './project-view/primitives';

/** Mesmo texto que o servidor registra no aceite. */
const DECLARATION =
  'Declaro, sob as penas da lei, que as informações e os documentos apresentados neste projeto são verdadeiros e que respondo por eles.';

const dateTime = new Intl.DateTimeFormat('pt-BR', {
  dateStyle: 'short',
  timeStyle: 'short',
  timeZone: 'America/Cuiaba'
});

function Summary({ project }: { project: Project }) {
  const { institution: i, proposal: p, funding: f, documents: d } = project;
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>Resumo do projeto</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className='space-y-2'>
          <Row label='Instrumento'>{i.instrument ? instrumentLabels[i.instrument] : ''}</Row>
          <Row label='Título'>{p.title}</Row>
          <Row label='Nº no SIGCon'>{p.sigconNumber}</Row>
          <Row label='Instituição'>
            {i.name}
            {i.cnpj && <span className='text-muted-foreground'> · {formatCnpj(i.cnpj)}</span>}
          </Row>
          <ExecutionRows proposal={p} />
          <Row label='Tipo de recurso'>{f.type ? fundingTypeLabels[f.type] : ''}</Row>
          <AmendmentRows funding={f} />
          <Row label='Total solicitado'>{f.totalCents ? formatBrl(f.totalCents) : ''}</Row>
          <Row label='Contrapartida'>{f.counterpartCents ? formatBrl(f.counterpartCents) : ''}</Row>
          <Row label='Documentos'>{documentsLine(d)}</Row>
        </dl>
      </CardContent>
    </Card>
  );
}

/** Projeto já enviado: código, data, etapa e o comprovante. */
function Submitted({ project }: { project: Project }) {
  const receipt = useMutation({
    mutationFn: async () => {
      const blob = await downloadReceipt(project.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `comprovante-${project.code}.pdf`;
      a.click();
      URL.revokeObjectURL(url);
    },
    onError: (err) => toast.error(errorMessage(err))
  });

  return (
    <div className='space-y-4'>
      <Card>
        <CardHeader>
          <CardDescription>Projeto recebido pela SEDEC</CardDescription>
          <CardTitle
            className='font-mono text-2xl'
            aria-label={`Código do projeto ${project.code}`}
          >
            {project.code}
          </CardTitle>
        </CardHeader>
        <CardContent className='space-y-3'>
          <p className='text-sm'>
            Enviado em {project.submittedAt ? dateTime.format(new Date(project.submittedAt)) : ''}
            {project.status === 'archived' ? (
              <>
                {' · '}
                <Badge variant='outline'>Arquivado</Badge>
              </>
            ) : (
              project.stage && (
                <>
                  {' · '}
                  <Badge variant='secondary'>{stageLabels[project.stage]}</Badge>
                </>
              )
            )}
          </p>
          <LoadingButton loading={receipt.isPending} onClick={() => receipt.mutate()}>
            <Icons.fileTypePdf aria-hidden /> Baixar comprovante (PDF)
          </LoadingButton>
          <p className='text-muted-foreground text-sm'>
            O comprovante atesta o recebimento. O número de protocolo oficial (SIGADOC) será
            informado depois da conferência dos documentos pela equipe da SEDEC. Uma confirmação
            também foi enviada ao seu e-mail.
          </p>
        </CardContent>
      </Card>
      {project.lateSubmission && (
        <Alert>
          <AlertTitle>Enviado depois da data-limite de protocolo</AlertTitle>
          <AlertDescription>
            O projeto segue apenas com atos preparatórios e preliminares; o repasse de recursos fica
            para depois do período de restrição eleitoral.
          </AlertDescription>
        </Alert>
      )}
      <Summary project={project} />
    </div>
  );
}

export function ReviewStep({
  project,
  editable,
  access,
  onGo
}: {
  project: Project;
  editable: boolean;
  access: Access;
  onGo: (section: SectionKey, focus?: string) => void;
}) {
  const qc = useQueryClient();
  const [accepted, setAccepted] = useState(false);
  const [confirming, setConfirming] = useState(false);

  // Depois do envio, só se reenvia durante uma complementação aberta; fora disso, a tela mostra o projeto recebido.
  const resubmitting = project.status === 'submitted' && access.complement !== null;
  // Itens devolvidos em que o proponente ainda não mexeu: o reenvio é permitido (a equipe confere), mas com aviso e confirmação.
  const unanswered = resubmitting
    ? (access.complement?.items.filter((i) => !i.answered) ?? [])
    : [];
  const blocks = project.issues.filter((i) => i.severity === 'block');
  const warns = project.issues.filter((i) => i.severity === 'warn');
  const sections = (Object.keys(sectionLabels) as SectionKey[]).filter((s) =>
    blocks.some((i) => i.section === s)
  );

  const send = useMutation({
    mutationFn: () => {
      const input = { version: project.version, declaration: accepted };
      return resubmitting ? resubmitProject(project.id, input) : submitProject(project.id, input);
    },
    onSuccess: async (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      await qc.invalidateQueries({ queryKey: projectKeys.lists() });
      toast.success(
        resubmitting
          ? 'Correções enviadas. O projeto voltou para a análise.'
          : `Projeto enviado. Código ${saved.code}.`
      );
    },
    onError: async (err) => {
      // Pendências novas ou projeto alterado em outra janela: recarrega para mostrar o estado real.
      if (err instanceof ApiError && (err.status === 409 || err.code === 'project_has_issues')) {
        await qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
      }
    },
    onSettled: () => setConfirming(false)
  });

  if (project.status !== 'draft' && !resubmitting) return <Submitted project={project} />;

  return (
    <div className='space-y-4'>
      <Summary project={project} />

      {blocks.length > 0 ? (
        <Alert variant='destructive'>
          <AlertTitle>Ainda há pendências que impedem o envio</AlertTitle>
          <AlertDescription>
            <ul className='mt-1 space-y-2'>
              {sections.map((s) => (
                <li key={s}>
                  <button
                    type='button'
                    className='font-medium underline underline-offset-4'
                    onClick={() => onGo(s)}
                  >
                    {sectionLabels[s]}
                  </button>
                  <ul className='list-disc pl-5'>
                    {blocks
                      .filter((i) => i.section === s)
                      .map((i) => (
                        <li key={`${i.field}-${i.code}`}>{i.message}</li>
                      ))}
                  </ul>
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      ) : (
        <Alert>
          <Icons.circleCheck aria-hidden />
          <AlertTitle>Tudo certo para enviar</AlertTitle>
          <AlertDescription>
            Nenhuma pendência impede o envio. Confira o resumo e aceite a declaração.
          </AlertDescription>
        </Alert>
      )}
      {unanswered.length > 0 && (
        <Alert>
          <Icons.alertCircle aria-hidden />
          <AlertTitle>Itens devolvidos que você ainda não alterou</AlertTitle>
          <AlertDescription>
            <ul className='mt-1 space-y-1'>
              {unanswered.map((i) => (
                <li key={`${i.kind}-${i.target}`}>
                  <button
                    type='button'
                    className='font-medium underline underline-offset-4'
                    onClick={() => onGo(stepOfItem(i), focusIdOf(i))}
                  >
                    {i.label}
                  </button>
                  {i.note && <span className='text-muted-foreground'>: {i.note}</span>}
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}
      {warns.length > 0 && (
        <Alert>
          <AlertTitle>Avisos (não impedem o envio)</AlertTitle>
          <AlertDescription>
            <ul className='list-disc pl-5'>
              {warns.map((i) => (
                <li key={`${i.field}-${i.code}`}>{i.message}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle className='text-base'>Declaração de veracidade</CardTitle>
        </CardHeader>
        <CardContent className='space-y-4'>
          <label className='flex items-start gap-3 text-sm'>
            <input
              type='checkbox'
              className='mt-1 size-4'
              checked={accepted}
              disabled={!editable || blocks.length > 0}
              onChange={(e) => setAccepted(e.target.checked)}
            />
            <span>{DECLARATION}</span>
          </label>
          <p className='text-muted-foreground text-xs'>
            O aceite é registrado com a data, a hora e o endereço IP do envio. Depois de enviado, o
            projeto não pode mais ser editado por você; se a equipe pedir complementação, a sua tela
            será reaberta nos itens solicitados.
          </p>
          {send.isError && (
            <Alert variant='destructive' role='alert'>
              <AlertDescription>{errorMessage(send.error)}</AlertDescription>
            </Alert>
          )}
          <Button
            disabled={!editable || blocks.length > 0 || !accepted || send.isPending}
            onClick={() => setConfirming(true)}
          >
            <Icons.send aria-hidden />{' '}
            {resubmitting ? 'Reenviar com as correções' : 'Enviar projeto'}
          </Button>
        </CardContent>
      </Card>

      <AlertModal
        isOpen={confirming}
        onClose={() => setConfirming(false)}
        onConfirm={() => send.mutate()}
        loading={send.isPending}
        title={resubmitting ? 'Reenviar o projeto com as correções?' : 'Enviar o projeto à SEDEC?'}
        description={
          resubmitting
            ? `${
                unanswered.length > 0
                  ? `Você ainda não alterou: ${unanswered.map((i) => i.label).join(', ')}. `
                  : ''
              }O projeto volta para a análise da equipe e você não poderá alterar mais nada até um novo pedido de correção.`
            : 'Depois de enviado, o projeto e os documentos não poderão mais ser alterados por você. Confira se está tudo certo.'
        }
        confirmLabel={resubmitting ? 'Reenviar' : 'Enviar projeto'}
        cancelLabel='Voltar'
      />
    </div>
  );
}
