'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { LoadingButton } from '@/components/ui/loading-button';
import { Skeleton } from '@/components/ui/skeleton';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';
import { filePreviewQueryOptions, projectKeys } from '../../api/queries';
import { getFileDownload, markFileRead, markFileViewed, reviewItem } from '../../api/service';
import type { Project, ReviewDecision, ReviewItem } from '../../api/types';
import { staffErrorMessage, reportStaffError } from './errors';
import { DocumentViewer } from './viewer/document-viewer';
import { ViewerError } from './viewer/viewer-error';

/** Tamanho máximo da justificativa de uma reprovação (o mesmo do servidor). */
export const MAX_REJECTION_NOTE = 500;

/** Baixa o arquivo original (o Word continua Word): abre a URL de download, que o navegador trata como anexo. */
export async function downloadOriginal(projectId: string, fileId: string) {
  const { url } = await getFileDownload(projectId, fileId);
  const a = document.createElement('a');
  a.href = url;
  a.rel = 'noopener';
  a.click();
}

function StatusBadge({ item }: { item: ReviewItem }) {
  if (item.status === 'approved') return <Badge variant='secondary'>Aprovado</Badge>;
  if (item.status === 'rejected') return <Badge variant='destructive'>Reprovado</Badge>;
  return <Badge variant='outline'>Pendente</Badge>;
}

/**
 * Análise de um documento: o arquivo aparece no próprio modal, sem baixar, e o rodapé tem a decisão (aprovar, ou reprovar com
 * justificativa). A decisão só vale depois de ver todos os arquivos do item; o servidor confere.
 */
export function DocumentReviewDialog({
  project,
  item,
  canReview,
  initialFileId,
  open,
  onOpenChange
}: {
  project: Project;
  item: ReviewItem;
  canReview: boolean;
  /** Arquivo a abrir primeiro (quem chega pelo botão de um arquivo); sem ele abre o primeiro ainda não visto. */
  initialFileId?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const firstPending = item.files.find((f) => !f.opened) ?? item.files[0];
  const [chosen, setChosen] = useState<string | undefined>();
  const wanted = [chosen, initialFileId].find((id) => item.files.some((f) => f.id === id));
  const fileId = wanted ?? firstPending?.id;
  const file = item.files.find((f) => f.id === fileId);
  const [rejecting, setRejecting] = useState(false);
  const [note, setNote] = useState('');
  const text = note.trim();

  const preview = useQuery({
    ...filePreviewQueryOptions(project.id, fileId ?? ''),
    enabled: open && !!fileId
  });

  // O registro de "visto" sai uma vez por arquivo, depois que o conteúdo apareceu.
  const sent = useRef(new Set<string>());
  const viewed = useMutation({
    mutationFn: (id: string) => markFileViewed(project.id, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) }),
    onError: (err, id) => {
      sent.current.delete(id);
      reportStaffError(err);
    }
  });
  const handleShown = () => {
    if (!file || file.opened || sent.current.has(file.id)) return;
    sent.current.add(file.id);
    viewed.mutate(file.id);
  };

  // Chegar ao fim do arquivo é a prova para aprovar (o servidor também confere); só ver basta para reprovar.
  const sentRead = useRef(new Set<string>());
  const readToEnd = useMutation({
    mutationFn: (id: string) => markFileRead(project.id, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) }),
    onError: (err, id) => {
      sentRead.current.delete(id);
      reportStaffError(err);
    }
  });
  const handleEnd = () => {
    if (!file || file.read || sentRead.current.has(file.id)) return;
    sentRead.current.add(file.id);
    readToEnd.mutate(file.id);
  };

  const decide = useMutation({
    mutationFn: (v: { decision: ReviewDecision; note?: string }) =>
      reviewItem(project.id, { kind: item.kind, target: item.target, ...v }),
    onSuccess: (saved, v) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      void qc.invalidateQueries({ queryKey: projectKeys.lists() });
      toast.success(
        v.decision === 'approved'
          ? 'Documento aprovado.'
          : v.decision === 'rejected'
            ? 'Documento reprovado.'
            : 'Decisão desfeita.'
      );
      if (v.decision !== 'pending') onOpenChange(false);
      setRejecting(false);
      setNote('');
    },
    onError: async (err) => {
      if (reportStaffError(err).reload)
        await qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
    }
  });

  const unseen = item.files.filter((f) => !f.opened).length;
  const unread = item.files.filter((f) => !f.read).length;
  const canDecide = canReview && !item.blocking;
  const mustSee = unseen > 0;
  const mustRead = unread > 0;
  const download = () =>
    fileId &&
    downloadOriginal(project.id, fileId).catch((err: unknown) => {
      toast.error(staffErrorMessage(err));
    });
  const reload = () => void preview.refetch();
  const reset = (next: boolean) => {
    if (!next) {
      setRejecting(false);
      setNote('');
      setChosen(undefined);
    }
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={reset}>
      <DialogContent className='flex h-[92vh] max-h-[92vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl'>
        <DialogHeader className='gap-1 border-b p-4 pr-12'>
          <div className='flex flex-wrap items-center gap-2'>
            <DialogTitle className='text-base'>{item.label}</DialogTitle>
            <StatusBadge item={item} />
          </div>
          <DialogDescription>
            {file ? file.filename : 'Documento sem arquivo.'}
            {preview.data?.converted && ' · Exibido como PDF (convertido do Word).'}
          </DialogDescription>
        </DialogHeader>

        <div className='flex min-h-0 flex-1 flex-col md:flex-row'>
          {item.files.length > 1 && (
            <nav
              aria-label='Arquivos do documento'
              className='flex shrink-0 gap-2 overflow-x-auto border-b p-2 md:w-60 md:flex-col md:overflow-y-auto md:border-r md:border-b-0'
            >
              {item.files.map((f) => (
                <button
                  key={f.id}
                  type='button'
                  onClick={() => setChosen(f.id)}
                  aria-current={f.id === fileId ? 'true' : undefined}
                  className={cn(
                    'flex min-w-40 items-center gap-2 rounded-md border px-2 py-1.5 text-left text-sm transition-colors md:min-w-0',
                    f.id === fileId ? 'border-primary bg-primary/5' : 'hover:bg-muted'
                  )}
                >
                  {f.read ? (
                    <Icons.circleCheck className='size-4 shrink-0 text-green-600' aria-hidden />
                  ) : f.opened ? (
                    <Icons.eye className='text-muted-foreground size-4 shrink-0' aria-hidden />
                  ) : (
                    <Icons.circle className='text-muted-foreground size-4 shrink-0' aria-hidden />
                  )}
                  <span className='min-w-0 flex-1 truncate'>{f.filename}</span>
                  <span className='sr-only'>
                    {f.read
                      ? 'Lido até o fim'
                      : f.opened
                        ? 'Visto, falta ler até o fim'
                        : 'Ainda não visto'}
                  </span>
                </button>
              ))}
            </nav>
          )}
          <div className='min-h-0 min-w-0 flex-1'>
            {!file ? (
              <ViewerError message='Este item não tem arquivo para exibir.' />
            ) : preview.isError ? (
              <ViewerError
                message={staffErrorMessage(preview.error)}
                onRetry={reload}
                onDownload={() => void download()}
              />
            ) : preview.isLoading || !preview.data ? (
              <Skeleton className='mx-auto my-4 h-[70vh] w-11/12' />
            ) : (
              <DocumentViewer
                preview={preview.data}
                filename={file.filename}
                onShown={handleShown}
                onReachedEnd={handleEnd}
                onRetry={reload}
                onDownload={() => void download()}
              />
            )}
          </div>
        </div>

        <div className='bg-muted/50 space-y-3 border-t p-4'>
          {item.status === 'rejected' && item.note && (
            <Alert variant='destructive'>
              <AlertDescription>
                <span className='font-medium'>Motivo da reprovação:</span> {item.note}
              </AlertDescription>
            </Alert>
          )}
          {rejecting && (
            <div className='space-y-1.5'>
              <Label htmlFor='review-reject-note'>Justificativa da reprovação *</Label>
              <Textarea
                id='review-reject-note'
                value={note}
                onChange={(e) => setNote(e.target.value)}
                rows={3}
                maxLength={MAX_REJECTION_NOTE}
                autoFocus
                aria-describedby='review-reject-hint'
                placeholder='Diga o que está errado neste documento. O texto pode seguir como observação ao proponente.'
              />
              <p id='review-reject-hint' className='text-muted-foreground text-xs'>
                {text.length} de {MAX_REJECTION_NOTE} caracteres.
              </p>
            </div>
          )}
          {canDecide && mustRead && item.status !== 'approved' && (
            <p className='text-muted-foreground text-sm' aria-live='polite'>
              {mustSee ? 'Veja os arquivos do documento para poder reprovar. ' : ''}
              Role até o fim {unread === 1 ? 'do arquivo' : `dos ${unread} arquivos`} para liberar{' '}
              <strong>Aprovar</strong>.
            </p>
          )}
          <div className='flex flex-col-reverse gap-2 sm:flex-row sm:flex-wrap sm:justify-end'>
            <Button variant='outline' onClick={download} disabled={!fileId}>
              <Icons.download aria-hidden /> Baixar original
            </Button>
            <span className='sm:flex-1' />
            {!canDecide && (
              <Button variant='outline' onClick={() => reset(false)}>
                Fechar
              </Button>
            )}
            {canDecide && rejecting && (
              <>
                <Button
                  variant='outline'
                  onClick={() => {
                    setRejecting(false);
                    setNote('');
                  }}
                >
                  Cancelar
                </Button>
                <LoadingButton
                  variant='destructive'
                  loading={decide.isPending}
                  disabled={mustSee || text.length === 0}
                  onClick={() => decide.mutate({ decision: 'rejected', note: text })}
                >
                  Confirmar reprovação
                </LoadingButton>
              </>
            )}
            {canDecide && !rejecting && (
              <>
                {item.status !== 'pending' && (
                  <LoadingButton
                    variant='outline'
                    loading={decide.isPending}
                    onClick={() => decide.mutate({ decision: 'pending' })}
                  >
                    Desfazer {item.status === 'approved' ? 'aprovação' : 'reprovação'}
                  </LoadingButton>
                )}
                {item.status !== 'rejected' && item.kind !== 'extra' && (
                  <Button variant='outline' disabled={mustSee} onClick={() => setRejecting(true)}>
                    Reprovar
                  </Button>
                )}
                {item.status !== 'approved' && (
                  <LoadingButton
                    loading={decide.isPending}
                    disabled={mustRead}
                    onClick={() => decide.mutate({ decision: 'approved' })}
                  >
                    Aprovar
                  </LoadingButton>
                )}
              </>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
