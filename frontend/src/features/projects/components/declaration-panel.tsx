'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog';
import { LoadingButton } from '@/components/ui/loading-button';
import { ApiError } from '@/lib/api/errors';
import { errorMessage } from '@/lib/errors';
import { declarationQueryOptions, invalidateAfterSignature, projectKeys } from '../api/queries';
import { acceptDeclaration, cancelDeclaration } from '../api/service';
import { sectionLabels, type ChecklistItem, type Declaration, type SectionKey } from '../api/types';
import { staleMessage, stepOfIssue } from '../declaration-hints';

const dateTime = (iso: string) =>
  new Date(iso).toLocaleString('pt-BR', {
    timeZone: 'America/Cuiaba',
    dateStyle: 'short',
    timeStyle: 'short'
  });

const AWARE =
  'Li o texto acima e declaro que as informações são verdadeiras, ciente da responsabilidade civil, penal e administrativa.';

/**
 * Declaração gerada pelo sistema: mostra o texto oficial preenchido com o cadastro, recebe o aceite e acompanha a assinatura do
 * representante legal no MT-ID. Cada declaração tem o seu pedido e o seu arquivo. O anexo manual segue disponível no item.
 */
export function DeclarationPanel({
  projectId,
  item,
  editable,
  onGoStep
}: {
  projectId: string;
  item: ChecklistItem;
  editable: boolean;
  /** Leva a pessoa à etapa do cadastro onde se corrige uma pendência da declaração. */
  onGoStep?: (step: SectionKey) => void;
}) {
  const qc = useQueryClient();
  const query = useQuery(declarationQueryOptions(projectId, item.code));
  const [open, setOpen] = useState(false);
  const decl = query.data;
  const current = decl?.current ?? null;

  // A assinatura terminou (ou o pedido mudou de situação) enquanto a tela estava aberta: o checklist, o resumo de documentos e as
  // pendências do projeto dependem disso, e só o painel sabe. Sem isto o painel diria "Assinada" e o item continuaria pendente.
  const status = current?.status;
  const lastStatus = useRef(status);
  useEffect(() => {
    if (lastStatus.current !== undefined && status !== lastStatus.current) {
      void invalidateAfterSignature(qc, projectId);
    }
    lastStatus.current = status;
  }, [status, qc, projectId]);

  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: projectKeys.declaration(projectId, item.code) }),
      qc.invalidateQueries({ queryKey: projectKeys.checklist(projectId) }),
      qc.invalidateQueries({ queryKey: projectKeys.detail(projectId) })
    ]);
  };
  const cancel = useMutation({
    mutationFn: () => cancelDeclaration(projectId, item.code),
    onSuccess: async () => {
      toast.success('Pedido de assinatura cancelado.');
      await refresh();
    },
    onError: (err) => toast.error(errorMessage(err))
  });

  // Falha ao carregar a prévia (ex.: 409 se deixou de se aplicar): o anexo manual continua valendo.
  if (!decl) return null;

  // Ao abrir o diálogo o texto e as pendências são lidos de novo: o cadastro pode ter mudado em outra etapa.
  const openDialog = () => {
    setOpen(true);
    void query.refetch();
  };

  const waiting = current?.status === 'awaiting_signature';
  const signed = current?.status === 'signed';
  const canIssue = editable && (!current || !waiting) && !(signed && !current?.stale);

  return (
    <div className='bg-muted/40 space-y-2 rounded-md p-3 text-sm' data-slot='declaration-panel'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <p className='font-medium'>Gerar e assinar pelo sistema</p>
        {signed && !current?.stale && (
          <Badge variant='secondary'>
            <Icons.check aria-hidden /> Assinada em{' '}
            {dateTime(current.signedAt ?? current.acceptedAt)}
          </Badge>
        )}
      </div>

      {!current && (
        <p className='text-muted-foreground'>
          O sistema monta a declaração com os dados do cadastro. Você confere o texto, confirma e{' '}
          {decl.signerName || 'o representante legal'} assina pelo aplicativo MTCidadão.
        </p>
      )}

      {waiting && (
        <Alert>
          <Icons.clock aria-hidden />
          <AlertDescription>
            Aguardando a assinatura de {decl.signerName} (CPF {current.signerCpf}) no aplicativo
            MTCidadão. Pedido feito em {dateTime(current.acceptedAt)}.
            {!current.requestedBySigner &&
              ' O pedido foi enviado para o representante legal: só ele pode assinar.'}{' '}
            Esta página se atualiza quando a assinatura for concluída.
          </AlertDescription>
        </Alert>
      )}

      {current && ['rejected', 'expired', 'cancelled'].includes(current.status) && (
        <p className='text-muted-foreground'>
          {current.status === 'rejected' && 'O signatário recusou a assinatura.'}
          {current.status === 'expired' && 'O pedido de assinatura venceu sem ser assinado.'}
          {current.status === 'cancelled' && 'O pedido de assinatura foi cancelado.'} Você pode
          gerar a declaração de novo.
        </p>
      )}

      {signed && current.stale && (
        <Alert variant='destructive'>
          <Icons.alertCircle aria-hidden />
          <AlertDescription>{staleMessage(current.staleReasons, true)}</AlertDescription>
        </Alert>
      )}
      {waiting && current.stale && (
        <p className='text-muted-foreground'>{staleMessage(current.staleReasons, false)}</p>
      )}

      {editable && (
        <div className='flex flex-wrap gap-2'>
          {canIssue && (
            <Button size='sm' onClick={openDialog}>
              {current ? 'Gerar declaração de novo' : 'Conferir e gerar declaração'}
            </Button>
          )}
          {waiting && current.stale && (
            <Button size='sm' onClick={openDialog}>
              Gerar de novo com o texto atualizado
            </Button>
          )}
          {waiting && (
            <LoadingButton
              size='sm'
              variant='outline'
              loading={cancel.isPending}
              onClick={() => cancel.mutate()}
            >
              Cancelar pedido
            </LoadingButton>
          )}
        </div>
      )}

      <DeclarationDialog
        key={decl.paramsSha256}
        projectId={projectId}
        decl={decl}
        open={open}
        loading={query.isFetching}
        onOpenChange={setOpen}
        onGoStep={onGoStep}
        onDone={refresh}
      />
    </div>
  );
}

function DeclarationDialog({
  projectId,
  decl,
  open,
  loading,
  onOpenChange,
  onGoStep,
  onDone
}: {
  projectId: string;
  decl: Declaration;
  open: boolean;
  /** O texto está sendo lido de novo: não dá para confirmar o que ainda pode mudar. */
  loading: boolean;
  onOpenChange: (open: boolean) => void;
  onGoStep?: (step: SectionKey) => void;
  onDone: () => Promise<void>;
}) {
  const [aware, setAware] = useState(false);
  const accept = useMutation({
    mutationFn: () => acceptDeclaration(projectId, decl.code, decl.paramsSha256),
    onSuccess: async () => {
      toast.success(
        `Pedido de assinatura enviado. ${decl.signerName} assina pelo aplicativo MTCidadão.`
      );
      onOpenChange(false);
      await onDone();
    },
    onError: async (err) => {
      toast.error(errorMessage(err));
      // O cadastro mudou desde que o texto foi carregado: recarrega para a pessoa ler o texto atual.
      if (err instanceof ApiError && err.code === 'declaration_changed') await onDone();
    }
  });
  const blocked = decl.issues.length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{decl.name}</DialogTitle>
          <DialogDescription>
            {decl.annex} · confira o texto exatamente como será emitido.
          </DialogDescription>
        </DialogHeader>

        <article className='bg-background space-y-4 rounded-md border p-5 font-serif text-[15px] leading-relaxed'>
          <p className='text-muted-foreground text-center text-xs'>{decl.institution}</p>
          <h3 className='text-center font-semibold'>{decl.title}</h3>
          {decl.paragraphs.map((p) => (
            <p key={p} className='text-justify'>
              {p}
            </p>
          ))}
          <p>{decl.place || '—'}, na data da assinatura.</p>
          <div className='pt-4 text-center'>
            <p className='font-semibold'>{decl.signerName || '—'}</p>
            <p>
              {decl.signerRole || '—'} — {decl.institution}
            </p>
          </div>
        </article>

        {blocked && (
          <Alert variant='destructive'>
            <Icons.alertCircle aria-hidden />
            <AlertDescription>
              <p className='mb-1 font-medium'>Complete o cadastro para gerar a declaração:</p>
              <ul className='list-disc pl-4'>
                {decl.issues.map((i) => (
                  <li key={`${i.code}-${i.field}`}>{i.message}</li>
                ))}
              </ul>
              {onGoStep && (
                <div className='mt-3 flex flex-wrap gap-2'>
                  {Array.from(new Set(decl.issues.map((i) => stepOfIssue(i.field)))).map((step) => (
                    <Button
                      key={step}
                      type='button'
                      size='sm'
                      variant='outline'
                      onClick={() => {
                        onOpenChange(false);
                        onGoStep(step);
                      }}
                    >
                      Corrigir em {sectionLabels[step]}
                    </Button>
                  ))}
                </div>
              )}
            </AlertDescription>
          </Alert>
        )}

        {loading && (
          <p className='text-muted-foreground text-xs' role='status'>
            Atualizando o texto com o cadastro…
          </p>
        )}

        <label className='flex items-start gap-2 text-sm'>
          <Checkbox
            checked={aware}
            onCheckedChange={(on) => setAware(!!on)}
            disabled={blocked || loading}
            className='mt-0.5'
          />
          <span>{AWARE}</span>
        </label>
        <p className='text-muted-foreground text-xs'>
          Ao confirmar, o sistema gera o PDF e pede a assinatura de{' '}
          {decl.signerName || 'quem assina'} no MT-ID. A assinatura é feita no aplicativo MTCidadão.
        </p>

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            Cancelar
          </Button>
          <LoadingButton
            loading={accept.isPending}
            disabled={blocked || loading || !aware}
            onClick={() => accept.mutate()}
          >
            Confirmar e gerar declaração
          </LoadingButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
