'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { toast } from 'sonner';
import { z } from 'zod';
import { Icons } from '@/components/icons';
import { AlertModal } from '@/components/modal/alert-modal';
import { Button } from '@/components/ui/button';
import { FieldGroup } from '@/components/ui/field';
import { LoadingButton } from '@/components/ui/loading-button';
import { Progress } from '@/components/ui/progress';
import { useAppForm } from '@/lib/form';
import { errorMessage } from '@/lib/errors';
import { formatBytes } from '@/lib/utils';
import { ACCEPT_ATTR, contentTypeOf, validateFile } from '@/features/documents/lib/file-rules';
import { uploadToStorage } from '@/features/documents/lib/upload';
import { projectKeys } from '../api/queries';
import {
  confirmFileUpload,
  createExtraDocument,
  deleteExtraDocument,
  deleteProjectFile,
  getFileDownload,
  requestExtraFileUpload,
  updateExtraDocument
} from '../api/service';
import {
  MAX_EXTRA_DOCUMENTS,
  type ComplementItem,
  type ExtraDocument,
  type Project
} from '../api/types';
import type { Access } from '../access';
import { ProjectFileViewer, ViewFileButton, useFileViewer } from './project-file-viewer';
import { ReturnedNote } from './returned-note';
import { ServerErrors } from './server-errors';

const schema = z.object({
  title: z
    .string()
    .trim()
    .min(1, 'Informe o título do documento.')
    .max(120, 'Use até 120 caracteres.'),
  description: z.string().trim().max(1000, 'Use até 1.000 caracteres.')
});

function useRefresh(projectId: string) {
  const qc = useQueryClient();
  return async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: projectKeys.checklist(projectId) }),
      qc.invalidateQueries({ queryKey: projectKeys.detail(projectId) })
    ]);
  };
}

/**
 * Formulário de um documento adicional. Ao criar, pede também o arquivo (obrigatório): cria o documento, envia o arquivo e, se o
 * envio falhar, desfaz a criação para não deixar um documento vazio para trás. Ao editar, título e descrição; num documento já enviado
 * (devolvido pela equipe) dá também para trocar o arquivo, que é opcional.
 */
function ExtraForm({
  projectId,
  known,
  existing,
  onDone
}: {
  projectId: string;
  /** Documentos já existentes, para identificar qual acabou de ser criado. */
  known: ExtraDocument[];
  existing?: ExtraDocument;
  onDone: () => void;
}) {
  const refresh = useRefresh(projectId);
  const input = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [percent, setPercent] = useState<number | null>(null);

  // Troca de arquivo só no documento que já fez parte de uma versão enviada: tirar o único arquivo de um ainda não enviado apagaria o documento.
  const replaceable = !!existing?.locked;

  const upload = async (itemId: string, f: File) => {
    try {
      setPercent(0);
      const grant = await requestExtraFileUpload(projectId, itemId, {
        filename: f.name,
        contentType: contentTypeOf(f)!,
        sizeBytes: f.size
      });
      await uploadToStorage(grant.upload, f, setPercent);
      await confirmFileUpload(projectId, grant.file.id);
    } finally {
      setPercent(null);
    }
  };

  const save = useMutation({
    mutationFn: async (v: { title: string; description: string }) => {
      if (existing) {
        await updateExtraDocument(projectId, existing.id, v);
        if (file) {
          // O documento comporta um só arquivo: o anterior sai antes (o já enviado fica guardado na versão enviada).
          if (existing.file) await deleteProjectFile(projectId, existing.file.id);
          await upload(existing.id, file);
        }
        return;
      }
      if (!file) throw new Error('Anexe o arquivo do documento.');
      const before = new Set(known.map((x) => x.id));
      const created = await createExtraDocument(projectId, v);
      const item = created.extraDocuments.find((x) => !before.has(x.id));
      if (!item) throw new Error('Não foi possível criar o documento.');
      try {
        await upload(item.id, file);
      } catch (err) {
        await deleteExtraDocument(projectId, item.id).catch(() => undefined);
        throw err;
      }
    },
    onSuccess: async () => {
      toast.success(existing ? 'Documento atualizado.' : 'Documento adicionado.');
      onDone();
    },
    onSettled: refresh
  });

  const form = useAppForm({
    defaultValues: { title: existing?.title ?? '', description: existing?.description ?? '' },
    validators: { onSubmit: schema },
    onSubmit: ({ value }) => {
      if (!existing || file) {
        const problem = file ? validateFile(file) : 'Anexe o arquivo do documento.';
        setFileError(problem);
        if (problem) return;
      }
      return save
        .mutateAsync({ title: value.title.trim(), description: value.description.trim() })
        .catch(() => undefined);
    }
  });

  return (
    <form
      className='space-y-4 rounded-lg border p-4'
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.AppField
          name='title'
          children={(f) => <f.TextField label='Título do documento' required maxLength={120} />}
        />
        <form.AppField
          name='description'
          children={(f) => (
            <f.TextareaField label='Descrição (opcional)' rows={3} maxLength={1000} showCount />
          )}
        />
        {(!existing || replaceable) && (
          <div className='space-y-2'>
            <p className='text-sm font-medium'>
              {existing ? 'Novo arquivo (opcional)' : 'Arquivo *'}
            </p>
            <input
              ref={input}
              type='file'
              hidden
              accept={ACCEPT_ATTR}
              onChange={(e) => {
                setFile(e.target.files?.[0] ?? null);
                setFileError(null);
              }}
              aria-label='Escolher o arquivo do documento'
            />
            <div className='flex flex-wrap items-center gap-2 text-sm'>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={() => input.current?.click()}
              >
                <Icons.upload aria-hidden /> {file ? 'Trocar arquivo' : 'Escolher arquivo'}
              </Button>
              {file && (
                <span className='truncate'>
                  {file.name}{' '}
                  <span className='text-muted-foreground'>({formatBytes(file.size)})</span>
                </span>
              )}
            </div>
            {fileError && (
              <p role='alert' className='text-destructive text-sm'>
                {fileError}
              </p>
            )}
            {percent !== null && <Progress value={percent} aria-label='Envio do arquivo' />}
          </div>
        )}
        <ServerErrors error={save.error} />
        <div className='flex flex-wrap gap-2'>
          <LoadingButton type='submit' size='sm' loading={save.isPending}>
            {existing ? 'Salvar' : 'Adicionar documento'}
          </LoadingButton>
          <Button
            type='button'
            size='sm'
            variant='ghost'
            disabled={save.isPending}
            onClick={onDone}
          >
            Cancelar
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}

function ExtraCard({
  project,
  doc,
  editable,
  returned
}: {
  project: Project;
  doc: ExtraDocument;
  editable: boolean;
  /** O pedido de complementação aberto devolveu este documento. */
  returned?: ComplementItem;
}) {
  const projectId = project.id;
  const refresh = useRefresh(projectId);
  const viewer = useFileViewer();
  const [editing, setEditing] = useState(false);
  const [removing, setRemoving] = useState(false);
  const download = useMutation({
    mutationFn: async () => (await getFileDownload(projectId, doc.file!.id)).url,
    onSuccess: (url) => {
      const a = document.createElement('a');
      a.href = url;
      a.rel = 'noopener';
      a.click();
    },
    onError: (err) => toast.error(errorMessage(err))
  });
  const remove = useMutation({
    mutationFn: () => deleteExtraDocument(projectId, doc.id),
    onSuccess: async () => {
      setRemoving(false);
      await refresh();
    },
    onError: (err) => {
      toast.error(errorMessage(err));
      setRemoving(false);
    }
  });

  if (editing)
    return (
      <li>
        <ExtraForm
          projectId={projectId}
          known={[]}
          existing={doc}
          onDone={() => setEditing(false)}
        />
      </li>
    );
  return (
    <li id={`extra-${doc.id}`} className='scroll-mt-20 space-y-2 rounded-lg border p-4'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <p className='min-w-0 font-medium'>{doc.title}</p>
        {editable && (
          <span className='flex shrink-0 gap-1'>
            <Button
              size='sm'
              variant='ghost'
              onClick={() => setEditing(true)}
              aria-label={`Editar ${doc.title}`}
            >
              <Icons.edit aria-hidden />
            </Button>
            {!doc.locked && (
              <Button
                size='sm'
                variant='ghost'
                onClick={() => setRemoving(true)}
                aria-label={`Remover ${doc.title}`}
              >
                <Icons.trash aria-hidden />
              </Button>
            )}
          </span>
        )}
      </div>
      {returned && <ReturnedNote item={returned} />}
      {doc.description && (
        <p className='text-muted-foreground text-sm whitespace-pre-line'>{doc.description}</p>
      )}
      {doc.file ? (
        <div className='flex items-center justify-between gap-2 text-sm'>
          <span className='flex min-w-0 items-center gap-2'>
            <Icons.paperclip className='text-muted-foreground size-4 shrink-0' aria-hidden />
            <span className='truncate'>{doc.file.filename}</span>
            <span className='text-muted-foreground shrink-0'>
              {formatBytes(doc.file.sizeBytes)}
            </span>
          </span>
          {doc.file.status === 'confirmed' && (
            <span className='flex shrink-0 gap-1'>
              <ViewFileButton file={doc.file} onClick={() => viewer.show(doc.file!.id)} />
              <Button
                size='sm'
                variant='ghost'
                onClick={() => download.mutate()}
                aria-label={`Baixar ${doc.file.filename}`}
              >
                Baixar
              </Button>
            </span>
          )}
        </div>
      ) : (
        <p className='text-muted-foreground text-sm'>Sem arquivo.</p>
      )}
      <AlertModal
        isOpen={removing}
        onClose={() => setRemoving(false)}
        onConfirm={() => remove.mutate()}
        loading={remove.isPending}
        title={`Remover "${doc.title}"?`}
        description='O documento e o arquivo dele serão apagados.'
        confirmLabel='Remover'
        cancelLabel='Cancelar'
      />
      {doc.file && (
        <ProjectFileViewer
          project={project}
          kind='extra'
          target={doc.id}
          label={doc.title}
          files={[doc.file]}
          fileId={viewer.fileId}
          open={viewer.open}
          onOpenChange={viewer.setOpen}
        />
      )}
    </li>
  );
}

/** Último bloco da aba Documentos: até 5 documentos livres (título, descrição opcional e arquivo), sempre opcionais. */
export function ExtraDocuments({
  project,
  docs,
  access,
  onlyReturned
}: {
  project: Project;
  docs: ExtraDocument[];
  access: Access;
  /** Mostra só os devolvidos pela equipe (o filtro "só o que falta" da aba). */
  onlyReturned?: boolean;
}) {
  const [adding, setAdding] = useState(false);
  const canAdd = access.extra(false);
  const full = docs.length >= MAX_EXTRA_DOCUMENTS;
  const shown = onlyReturned ? docs.filter((d) => access.returned('extra', d.id)) : docs;

  return (
    <section aria-labelledby='group-extra' className='space-y-3'>
      <div>
        <h3 id='group-extra' className='text-base font-semibold'>
          Outros documentos
        </h3>
        <p className='text-muted-foreground text-sm'>
          Opcional. Anexe documentos que não estão na lista acima: título, descrição (opcional) e um
          arquivo. Até {MAX_EXTRA_DOCUMENTS} documentos ({docs.length} de {MAX_EXTRA_DOCUMENTS}
          ).
        </p>
      </div>
      {shown.length > 0 && (
        <ul className='space-y-3'>
          {shown.map((d) => (
            <ExtraCard
              key={`${d.id}-${d.title}-${d.description}-${d.file?.id}`}
              project={project}
              doc={d}
              editable={access.extra(d.locked, d.id)}
              returned={access.returned('extra', d.id)}
            />
          ))}
        </ul>
      )}
      {adding ? (
        <ExtraForm projectId={project.id} known={docs} onDone={() => setAdding(false)} />
      ) : (
        canAdd && (
          <div className='space-y-1'>
            <Button
              type='button'
              size='sm'
              variant='outline'
              disabled={full}
              onClick={() => setAdding(true)}
            >
              <Icons.plusCircle aria-hidden /> Adicionar outro documento
            </Button>
            {full && (
              <p className='text-muted-foreground text-xs'>
                Limite de {MAX_EXTRA_DOCUMENTS} documentos adicionais atingido.
              </p>
            )}
          </div>
        )
      )}
    </section>
  );
}
