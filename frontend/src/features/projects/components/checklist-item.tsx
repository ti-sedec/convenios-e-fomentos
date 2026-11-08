'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { AlertModal } from '@/components/modal/alert-modal';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { LoadingButton } from '@/components/ui/loading-button';
import { Progress } from '@/components/ui/progress';
import { Textarea } from '@/components/ui/textarea';
import { ApiError } from '@/lib/api/errors';
import { errorMessage } from '@/lib/errors';
import { formatBytes } from '@/lib/utils';
import { ACCEPT_ATTR, contentTypeOf, validateFile } from '@/features/documents/lib/file-rules';
import { uploadToStorage } from '@/features/documents/lib/upload';
import { projectKeys } from '../api/queries';
import {
  confirmFileUpload,
  deleteProjectFile,
  getFileDownload,
  requestFileUpload,
  saveJustification
} from '../api/service';
import {
  GENERATED_DECLARATIONS,
  type ChecklistItem,
  type ComplementItem,
  type DocFile,
  type Project,
  type SectionKey
} from '../api/types';
import { staleMessage } from '../declaration-hints';
import { DeclarationPanel } from './declaration-panel';
import { ProjectFileViewer, ViewFileButton, useFileViewer } from './project-file-viewer';
import { ReturnedNote } from './returned-note';

const MAX_FILES = 10;

/** Âncora do item na página: a aba Documentos leva o proponente até o que falta. */
export function itemDomId(item: Pick<ChecklistItem, 'code' | 'amendmentId'>) {
  return `doc-${item.code}${item.amendmentId ? `-${item.amendmentId}` : ''}`;
}

const statusBadge = {
  missing: { label: 'Pendente', variant: 'outline' as const },
  done: { label: 'Anexado', variant: 'secondary' as const },
  justified: { label: 'Justificado', variant: 'secondary' as const }
};

interface Transfer {
  id: number;
  name: string;
  percent: number;
  stage: 'sending' | 'confirming' | 'error';
  error?: string;
}

/** Uma exigência do checklist: arquivos anexados, envio de novos arquivos e justificativa de não envio. */
export function ChecklistItemCard({
  project,
  item,
  editable,
  returned,
  onGoStep
}: {
  project: Project;
  item: ChecklistItem;
  editable: boolean;
  /** Leva a pessoa a uma etapa do cadastro (as pendências das declarações apontam para onde corrigir). */
  onGoStep?: (step: SectionKey) => void;
  /** O pedido de complementação aberto devolveu este documento. */
  returned?: ComplementItem;
}) {
  const projectId = project.id;
  const viewer = useFileViewer();
  const qc = useQueryClient();
  const input = useRef<HTMLInputElement>(null);
  const seq = useRef(0);
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [toDelete, setToDelete] = useState<DocFile | null>(null);
  const [justifying, setJustifying] = useState(false);
  const [draft, setDraft] = useState(item.justification);
  // Opcional sem arquivo não é erro: "Não enviado", em tom neutro, em vez de "Pendente".
  const status =
    item.status === 'missing' && !item.mandatory
      ? { label: 'Não enviado', variant: 'outline' as const }
      : statusBadge[item.status];
  const amendmentId = item.amendmentId || undefined;

  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: projectKeys.checklist(projectId) }),
      qc.invalidateQueries({ queryKey: projectKeys.detail(projectId) })
    ]);
  };
  const patch = (id: number, change: Partial<Transfer>) =>
    setTransfers((all) => all.map((t) => (t.id === id ? { ...t, ...change } : t)));

  /** Fluxo completo de um arquivo: autoriza → envia ao armazenamento → confirma. */
  const upload = async (file: File) => {
    const id = ++seq.current;
    setTransfers((all) => [...all, { id, name: file.name, percent: 0, stage: 'sending' }]);
    let fileId: string | undefined;
    try {
      const problem = validateFile(file);
      if (problem) throw new Error(problem);
      const grant = await requestFileUpload(projectId, item.code, {
        filename: file.name,
        contentType: contentTypeOf(file)!,
        sizeBytes: file.size,
        amendmentId
      });
      fileId = grant.file.id;
      await uploadToStorage(grant.upload, file, (percent) => patch(id, { percent }));
      patch(id, { percent: 100, stage: 'confirming' });
      await confirmFileUpload(projectId, fileId);
      setTransfers((all) => all.filter((t) => t.id !== id));
    } catch (err) {
      patch(id, { stage: 'error', error: errorMessage(err) });
      // Não deixa um registro pendente para trás; uma recusa na confirmação (422) já apagou tudo no servidor.
      const rejectedByServer = err instanceof ApiError && err.status === 422;
      if (fileId && !rejectedByServer)
        await deleteProjectFile(projectId, fileId).catch(() => undefined);
    } finally {
      await refresh();
    }
  };

  const pick = (list: FileList | null) => {
    if (!list) return;
    const room = Math.max(MAX_FILES - item.files.length, 0);
    const files = Array.from(list);
    if (files.length > room) toast.error(`Este documento comporta mais ${room} arquivo(s).`);
    files.slice(0, room).forEach((f) => void upload(f));
    if (input.current) input.current.value = '';
  };

  const download = useMutation({
    mutationFn: async (f: DocFile) => (await getFileDownload(projectId, f.id)).url,
    onSuccess: (url) => {
      const a = document.createElement('a');
      a.href = url;
      a.rel = 'noopener';
      a.click();
    },
    onError: (err) => toast.error(errorMessage(err))
  });
  const remove = useMutation({
    mutationFn: (f: DocFile) => deleteProjectFile(projectId, f.id),
    onSuccess: async () => {
      setToDelete(null);
      await refresh();
    },
    onError: (err) => {
      toast.error(errorMessage(err));
      setToDelete(null);
    }
  });
  const justify = useMutation({
    mutationFn: (text: string) => saveJustification(projectId, item.code, { amendmentId, text }),
    onSuccess: async (_, text) => {
      toast.success(text.trim() ? 'Justificativa salva.' : 'Justificativa removida.');
      setJustifying(false);
      await refresh();
    },
    onError: (err) => toast.error(errorMessage(err))
  });

  return (
    <li id={itemDomId(item)} className='scroll-mt-20 space-y-3 rounded-lg border p-4'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <div className='min-w-0'>
          <p className='font-medium'>{item.name}</p>
          {item.amendmentLabel && (
            <p className='text-muted-foreground text-sm'>{item.amendmentLabel}</p>
          )}
        </div>
        <div className='flex items-center gap-2'>
          {item.source && (
            <Badge variant='outline' title='Item do checklist da SEDEC'>
              {item.source}
            </Badge>
          )}
          <Badge variant={item.mandatory ? 'default' : 'outline'}>
            {item.mandatory ? 'Obrigatório' : 'Opcional'}
          </Badge>
          <Badge variant={status.variant}>{status.label}</Badge>
        </div>
      </div>
      {returned && <ReturnedNote item={returned} />}
      {item.description && <p className='text-muted-foreground text-sm'>{item.description}</p>}
      {item.template && (
        <a
          href={item.template.url}
          target='_blank'
          rel='noopener noreferrer'
          className='inline-flex items-center gap-1 text-sm underline underline-offset-4'
        >
          <Icons.externalLink className='size-3.5' aria-hidden /> Baixar modelo (
          {item.template.name})
        </a>
      )}

      {item.declarationStale && item.status !== 'done' && (
        <p className='text-destructive text-sm'>
          {staleMessage(item.declarationStaleReasons, true)}
        </p>
      )}
      {GENERATED_DECLARATIONS.includes(item.code) && (
        <DeclarationPanel
          projectId={projectId}
          item={item}
          editable={editable}
          onGoStep={onGoStep}
        />
      )}

      {item.files.length > 0 && (
        <ul className='space-y-1' aria-label={`Arquivos de ${item.name}`}>
          {item.files.map((f) => (
            <li key={f.id} className='flex items-center justify-between gap-2 text-sm'>
              <span className='flex min-w-0 items-center gap-2'>
                <Icons.paperclip className='text-muted-foreground size-4 shrink-0' aria-hidden />
                <span className='truncate' title={f.sha256 ? `SHA-256: ${f.sha256}` : undefined}>
                  {f.filename}
                </span>
                <span className='text-muted-foreground shrink-0'>{formatBytes(f.sizeBytes)}</span>
                {f.status === 'pending' && <Badge variant='outline'>envio incompleto</Badge>}
              </span>
              <span className='flex shrink-0 gap-1'>
                {f.status === 'confirmed' && (
                  <>
                    <ViewFileButton file={f} onClick={() => viewer.show(f.id)} />
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => download.mutate(f)}
                      aria-label={`Baixar ${f.filename}`}
                    >
                      Baixar
                    </Button>
                  </>
                )}
                {editable && (
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => setToDelete(f)}
                    aria-label={`Remover ${f.filename}`}
                  >
                    <Icons.trash aria-hidden />
                  </Button>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}

      {transfers.map((t) => (
        <div key={t.id} className='space-y-1 text-sm' role='status'>
          <div className='flex justify-between gap-2'>
            <span className='truncate'>{t.name}</span>
            <span className='text-muted-foreground shrink-0'>
              {t.stage === 'error'
                ? 'Falhou'
                : t.stage === 'confirming'
                  ? 'Confirmando…'
                  : `${t.percent}%`}
            </span>
          </div>
          {t.stage === 'error' ? (
            <div className='flex items-center justify-between gap-2'>
              <p className='text-destructive'>{t.error}</p>
              <Button
                size='sm'
                variant='ghost'
                onClick={() => setTransfers((all) => all.filter((x) => x.id !== t.id))}
              >
                Dispensar
              </Button>
            </div>
          ) : (
            <Progress value={t.percent} aria-label={`Envio de ${t.name}`} />
          )}
        </div>
      ))}

      {editable && (
        <div className='flex flex-wrap items-center gap-2'>
          <input
            ref={input}
            type='file'
            multiple
            hidden
            accept={ACCEPT_ATTR}
            onChange={(e) => pick(e.target.files)}
            aria-label={`Escolher arquivos para ${item.name}`}
          />
          <Button
            type='button'
            size='sm'
            variant='outline'
            disabled={item.files.length >= MAX_FILES}
            onClick={() => input.current?.click()}
          >
            <Icons.upload aria-hidden /> Anexar arquivo
          </Button>
          {item.acceptsJustification && !justifying && (
            <Button
              type='button'
              size='sm'
              variant='ghost'
              onClick={() => {
                setDraft(item.justification);
                setJustifying(true);
              }}
            >
              {item.justification ? 'Editar justificativa' : 'Justificar não envio'}
            </Button>
          )}
        </div>
      )}

      {item.justification && !justifying && (
        <div className='bg-muted/40 rounded-md p-3 text-sm'>
          <p className='text-muted-foreground mb-1 text-xs font-medium'>
            Justificativa de não envio
          </p>
          <p className='whitespace-pre-line'>{item.justification}</p>
        </div>
      )}
      {justifying && (
        <div className='space-y-2'>
          <label className='text-sm font-medium' htmlFor={`just-${item.code}-${item.amendmentId}`}>
            Justificativa de não envio
          </label>
          <Textarea
            id={`just-${item.code}-${item.amendmentId}`}
            rows={3}
            maxLength={2000}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder='Explique por que o documento não está sendo enviado agora.'
          />
          <div className='flex flex-wrap gap-2'>
            <LoadingButton
              size='sm'
              loading={justify.isPending}
              onClick={() => justify.mutate(draft)}
            >
              Salvar justificativa
            </LoadingButton>
            {item.justification && (
              <Button
                size='sm'
                variant='outline'
                disabled={justify.isPending}
                onClick={() => justify.mutate('')}
              >
                Remover justificativa
              </Button>
            )}
            <Button size='sm' variant='ghost' onClick={() => setJustifying(false)}>
              Cancelar
            </Button>
          </div>
        </div>
      )}

      <AlertModal
        isOpen={toDelete !== null}
        onClose={() => setToDelete(null)}
        onConfirm={() => toDelete && remove.mutate(toDelete)}
        loading={remove.isPending}
        title={`Remover "${toDelete?.filename ?? ''}"?`}
        description='O arquivo será apagado. Você pode anexar outro depois.'
        confirmLabel='Remover'
        cancelLabel='Cancelar'
      />
      {item.files.length > 0 && (
        <ProjectFileViewer
          project={project}
          kind='document'
          target={item.amendmentId ? `${item.code}@${item.amendmentId}` : item.code}
          label={item.name}
          files={item.files}
          fileId={viewer.fileId}
          open={viewer.open}
          onOpenChange={viewer.setOpen}
        />
      )}
    </li>
  );
}
