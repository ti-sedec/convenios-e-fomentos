'use client';

import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { useCan } from '@/features/auth/session-provider';
import { errorMessage } from '@/lib/errors';
import { cn } from '@/lib/utils';
import { filePreviewQueryOptions } from '../api/queries';
import type { DocFile, Project, ReviewKind } from '../api/types';
import { DocumentReviewDialog, downloadOriginal } from './staff/document-review-dialog';
import { canReviewNow } from './staff/review-panel';
import { DocumentViewer } from './staff/viewer/document-viewer';
import { ViewerError } from './staff/viewer/viewer-error';

const noop = () => undefined;

/** Botão "Visualizar" de um arquivo da aba Documentos: abre o arquivo no próprio sistema, sem baixar. */
export function ViewFileButton({ file, onClick }: { file: DocFile; onClick: () => void }) {
  return (
    <Button size='sm' variant='ghost' onClick={onClick} aria-label={`Visualizar ${file.filename}`}>
      <Icons.eye aria-hidden /> Visualizar
    </Button>
  );
}

/** Estado de um visualizador por cartão: o arquivo fica guardado depois de fechar, para o modal não trocar de arquivo ao sumir. */
export function useFileViewer() {
  const [state, setState] = useState<{ open: boolean; fileId?: string }>({ open: false });
  return {
    open: state.open,
    fileId: state.fileId,
    show: (fileId: string) => setState({ open: true, fileId }),
    setOpen: (open: boolean) => setState((s) => ({ ...s, open }))
  };
}

/**
 * Exibe os arquivos de um documento do cadastro. A equipe abre o modal da conferência (o "visto" fica registrado e, para quem pode
 * analisar, a decisão está no rodapé); o proponente e os membros abrem o mesmo visualizador, sem decisão.
 */
export function ProjectFileViewer({
  project,
  kind,
  target,
  label,
  files,
  fileId,
  open,
  onOpenChange
}: {
  project: Project;
  kind: ReviewKind;
  /** Chave do documento (`codigo` ou `codigo@emenda`) ou id do documento adicional. */
  target: string;
  label: string;
  files: DocFile[];
  fileId?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const can = useCan();
  const item = project.review?.items.find((i) => i.kind === kind && i.target === target);
  if (item && item.files.length > 0)
    return (
      <DocumentReviewDialog
        project={project}
        item={item}
        canReview={canReviewNow(project, can)}
        initialFileId={fileId}
        open={open}
        onOpenChange={onOpenChange}
      />
    );
  return (
    <FilePreviewDialog
      projectId={project.id}
      label={label}
      files={files.filter((f) => f.status === 'confirmed')}
      initialFileId={fileId}
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}

/** Visualizador sem conferência: o arquivo no modal e o download do original. */
function FilePreviewDialog({
  projectId,
  label,
  files,
  initialFileId,
  open,
  onOpenChange
}: {
  projectId: string;
  label: string;
  files: DocFile[];
  initialFileId?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [chosen, setChosen] = useState<string | undefined>();
  const wanted = [chosen, initialFileId].find((id) => files.some((f) => f.id === id));
  const file = files.find((f) => f.id === wanted) ?? files[0];
  const preview = useQuery({
    ...filePreviewQueryOptions(projectId, file?.id ?? ''),
    enabled: open && !!file
  });
  const download = () =>
    file &&
    downloadOriginal(projectId, file.id).catch((err: unknown) => {
      toast.error(errorMessage(err));
    });
  const reload = () => void preview.refetch();

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setChosen(undefined);
        onOpenChange(next);
      }}
    >
      <DialogContent className='flex h-[92vh] max-h-[92vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl'>
        <DialogHeader className='gap-1 border-b p-4 pr-12'>
          <DialogTitle className='text-base'>{label}</DialogTitle>
          <DialogDescription>
            {file ? file.filename : 'Documento sem arquivo.'}
            {preview.data?.converted && ' · Exibido como PDF (convertido do Word).'}
          </DialogDescription>
        </DialogHeader>

        <div className='flex min-h-0 flex-1 flex-col md:flex-row'>
          {files.length > 1 && (
            <nav
              aria-label='Arquivos do documento'
              className='flex shrink-0 gap-2 overflow-x-auto border-b p-2 md:w-60 md:flex-col md:overflow-y-auto md:border-r md:border-b-0'
            >
              {files.map((f) => (
                <button
                  key={f.id}
                  type='button'
                  onClick={() => setChosen(f.id)}
                  aria-current={f.id === file?.id ? 'true' : undefined}
                  className={cn(
                    'flex min-w-40 items-center gap-2 rounded-md border px-2 py-1.5 text-left text-sm transition-colors md:min-w-0',
                    f.id === file?.id ? 'border-primary bg-primary/5' : 'hover:bg-muted'
                  )}
                >
                  <Icons.paperclip className='text-muted-foreground size-4 shrink-0' aria-hidden />
                  <span className='min-w-0 flex-1 truncate'>{f.filename}</span>
                </button>
              ))}
            </nav>
          )}
          <div className='min-h-0 min-w-0 flex-1'>
            {!file ? (
              <ViewerError message='Este documento não tem arquivo para exibir.' />
            ) : preview.isError ? (
              <ViewerError
                message={errorMessage(preview.error)}
                onRetry={reload}
                onDownload={() => void download()}
              />
            ) : preview.isLoading || !preview.data ? (
              <Skeleton className='mx-auto my-4 h-[70vh] w-11/12' />
            ) : (
              <DocumentViewer
                preview={preview.data}
                filename={file.filename}
                onShown={noop}
                onReachedEnd={noop}
                onRetry={reload}
                onDownload={() => void download()}
              />
            )}
          </div>
        </div>

        <div className='bg-muted/50 flex flex-col-reverse gap-2 border-t p-4 sm:flex-row sm:justify-between'>
          <Button variant='outline' onClick={() => void download()} disabled={!file}>
            <Icons.download aria-hidden /> Baixar original
          </Button>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            Fechar
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
