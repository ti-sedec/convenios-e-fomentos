'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { LoadingButton } from '@/components/ui/loading-button';
import { Progress } from '@/components/ui/progress';
import { projectKeys, sigadocQueryOptions } from '../../api/queries';
import { cancelSigadoc, openSigadoc, previewTermo, resumeSigadoc } from '../../api/service';
import {
  sigadocItemStatusLabels,
  type Project,
  type SigadocItem,
  type SigadocView
} from '../../api/types';
import { reportStaffError } from './errors';
import { isResponsible } from './review-panel';

/** Campos do Termo (nomes da entrevista do modelo no SIGADOC), na ordem em que fazem sentido para quem confere. */
const fieldLabels: Record<string, string> = {
  docGovMT_interessado_nome: 'Interessado (nome)',
  'docGovMT_cpf-cnpj': 'CPF/CNPJ',
  docGovMT_telefone: 'Telefone',
  docGovMT_email: 'E-mail',
  docGovMT_resumoAssunto: 'Resumo do assunto',
  auxGovMT_interessadoProcurador: 'Solicitante (interessado ou procurador)',
  docGovMT_anexoFisico: 'Possui anexo físico (Sim ou Nao)',
  auxGovMT_deveJuntar: 'Juntar a processo existente (sim ou não)'
};
const fieldOrder = Object.keys(fieldLabels);

function PinField({ pin, onChange }: { pin: string; onChange: (v: string) => void }) {
  return (
    <div className='space-y-2'>
      <Label htmlFor='sigadoc-pin'>PIN de assinatura do SIGADOC</Label>
      <Input
        id='sigadoc-pin'
        type='password'
        inputMode='numeric'
        autoComplete='off'
        value={pin}
        onChange={(e) => onChange(e.target.value)}
        aria-describedby='sigadoc-pin-help'
      />
      <p id='sigadoc-pin-help' className='text-muted-foreground text-xs'>
        O PIN é usado uma única vez para assinar tudo e nunca é guardado. Confira-o antes de enviar:
        tentativas erradas podem bloquear o seu PIN no SIGADOC.
      </p>
    </div>
  );
}

/** Assistente de abertura: 1) confere o Termo, 2) informa o PIN. O PIN só existe neste componente e some ao fechar. */
function OpenDialog({ project, onClose }: { project: Project; onClose: () => void }) {
  const qc = useQueryClient();
  const [step, setStep] = useState<'termo' | 'pin'>('termo');
  const [fields, setFields] = useState<Record<string, string>>({});
  const [pin, setPin] = useState('');
  const preview = useMutation({
    mutationFn: (f: Record<string, string>) => previewTermo(project.id, f),
    onSuccess: (p) => setFields(p.fields),
    onError: (err) => void reportStaffError(err)
  });
  // gcTime 0: a mutação (e o PIN nas variáveis) não fica no cache depois de terminar.
  const open = useMutation({
    mutationFn: () => openSigadoc(project.id, { pin, fields }),
    gcTime: 0,
    onSuccess: (view) => {
      qc.setQueryData(projectKeys.sigadoc(project.id), view);
      toast.success('Abertura iniciada. O sistema monta o processo em segundo plano.');
      setPin('');
      onClose();
    },
    onError: (err) => {
      setPin('');
      void reportStaffError(err);
    }
  });
  const html = preview.data?.html;
  const keys = [
    ...fieldOrder.filter((k) => k in fields),
    ...Object.keys(fields).filter((k) => !fieldOrder.includes(k))
  ];

  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) {
          setPin('');
          onClose();
        }
      }}
    >
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>
            {step === 'termo' ? '1. Conferir o Termo de Abertura' : '2. Informar o PIN'}
          </DialogTitle>
          <DialogDescription>
            {step === 'termo'
              ? 'O sistema preencheu o Termo com os dados do projeto. Ajuste o que precisar e confira a pré-visualização. Nada foi enviado ao SIGADOC ainda.'
              : 'Com o PIN, o sistema assina e autua o Termo, depois inclui o relatório e os documentos do projeto no processo, tudo em segundo plano.'}
          </DialogDescription>
        </DialogHeader>
        {step === 'termo' ? (
          <div className='space-y-4'>
            {!html ? (
              <div className='space-y-2'>
                <p className='text-muted-foreground text-sm'>
                  Gere a pré-visualização para ver e ajustar o Termo.
                </p>
                <LoadingButton loading={preview.isPending} onClick={() => preview.mutate({})}>
                  Gerar pré-visualização
                </LoadingButton>
              </div>
            ) : (
              <>
                <div className='grid gap-3 sm:grid-cols-2'>
                  {keys.map((k) => (
                    <div key={k} className='space-y-1'>
                      <Label htmlFor={`termo-${k}`}>{fieldLabels[k] ?? k}</Label>
                      <Input
                        id={`termo-${k}`}
                        value={fields[k] ?? ''}
                        onChange={(e) => setFields((f) => ({ ...f, [k]: e.target.value }))}
                      />
                    </div>
                  ))}
                </div>
                <LoadingButton
                  variant='outline'
                  loading={preview.isPending}
                  onClick={() => preview.mutate(fields)}
                >
                  Atualizar pré-visualização
                </LoadingButton>
                {/* sandbox sem permissões: o HTML do modelo é só para ler. */}
                <iframe
                  title='Pré-visualização do Termo de Abertura'
                  sandbox=''
                  srcDoc={html}
                  className='bg-background h-72 w-full rounded-md border'
                />
              </>
            )}
          </div>
        ) : (
          <PinField pin={pin} onChange={setPin} />
        )}
        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => {
              setPin('');
              if (step === 'pin') setStep('termo');
              else onClose();
            }}
          >
            {step === 'pin' ? 'Voltar ao Termo' : 'Cancelar'}
          </Button>
          {step === 'termo' ? (
            <Button disabled={!html} onClick={() => setStep('pin')}>
              Continuar
            </Button>
          ) : (
            <LoadingButton
              loading={open.isPending}
              disabled={pin.trim().length < 4}
              onClick={() => open.mutate()}
            >
              Abrir processo no SIGADOC
            </LoadingButton>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ResumeDialog({ project, onClose }: { project: Project; onClose: () => void }) {
  const qc = useQueryClient();
  const [pin, setPin] = useState('');
  const resume = useMutation({
    mutationFn: () => resumeSigadoc(project.id, pin),
    gcTime: 0,
    onSuccess: (view) => {
      qc.setQueryData(projectKeys.sigadoc(project.id), view);
      toast.success('Montagem retomada.');
      setPin('');
      onClose();
    },
    onError: (err) => {
      setPin('');
      void reportStaffError(err);
    }
  });
  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) {
          setPin('');
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Retomar a montagem do processo</DialogTitle>
          <DialogDescription>
            Informe o PIN de novo. O que já foi criado, assinado e incluído no processo não é
            refeito.
          </DialogDescription>
        </DialogHeader>
        <PinField pin={pin} onChange={setPin} />
        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            Cancelar
          </Button>
          <LoadingButton
            loading={resume.isPending}
            disabled={pin.trim().length < 4}
            onClick={() => resume.mutate()}
          >
            Retomar
          </LoadingButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ItemRow({ it }: { it: SigadocItem }) {
  const part = it.parts > 1 ? ` (parte ${it.part} de ${it.parts})` : '';
  return (
    <li className='flex flex-wrap items-center gap-2 py-1 text-sm'>
      <Badge
        variant={
          it.status === 'failed' ? 'destructive' : it.status === 'joined' ? 'secondary' : 'outline'
        }
      >
        {sigadocItemStatusLabels[it.status]}
      </Badge>
      <span>
        {it.label}
        {part}
      </span>
      {it.error && <span className='text-muted-foreground text-xs'>{it.error}</span>}
    </li>
  );
}

function CancelButton({ project }: { project: Project }) {
  const qc = useQueryClient();
  const cancel = useMutation({
    mutationFn: () => cancelSigadoc(project.id),
    onSuccess: (view: SigadocView) => {
      qc.setQueryData(projectKeys.sigadoc(project.id), view);
      toast.success('Cancelamento pedido. O que já foi inserido permanece no SIGADOC.');
    },
    onError: (err) => void reportStaffError(err)
  });
  return (
    <LoadingButton variant='outline' loading={cancel.isPending} onClick={() => cancel.mutate()}>
      Cancelar a montagem
    </LoadingButton>
  );
}

/**
 * Abertura do processo no SIGADOC, do analista responsável: abrir (Termo e PIN), acompanhar o andamento, retomar uma montagem
 * interrompida e cancelar. Só aparece na etapa de abertura; depois de pronto, a tramitação fica no histórico.
 */
export function SigadocOpenPanel({ project }: { project: Project }) {
  const q = useQuery({
    ...sigadocQueryOptions(project.id),
    enabled: project.status === 'submitted' && project.stage === 'opening_sigadoc'
  });
  const [dialog, setDialog] = useState<'open' | 'resume' | null>(null);
  const qc = useQueryClient();
  const status = q.data?.task?.status;
  const before = useRef(status);
  // Quando a montagem termina o projeto muda de etapa no servidor ("Aguardando parecer"): recarrega o detalhe e as filas.
  useEffect(() => {
    if (before.current === 'running' && status && status !== 'running') {
      void qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
      void qc.invalidateQueries({ queryKey: projectKeys.lists() });
      void qc.invalidateQueries({ queryKey: projectKeys.history(project.id) });
    }
    before.current = status;
  }, [status, project.id, qc]);
  if (project.status !== 'submitted' || project.stage !== 'opening_sigadoc') return null;
  const view = q.data;
  if (!view) return null;
  const responsible = isResponsible(project);
  const task = view.task;
  const running = task?.status === 'running';
  const interrupted = task?.status === 'interrupted';
  const pct =
    task && task.progressTotal > 0 ? Math.round((task.progressDone / task.progressTotal) * 100) : 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2 text-base'>
          <Icons.send aria-hidden /> Abertura do processo no SIGADOC
        </CardTitle>
      </CardHeader>
      <CardContent className='space-y-3'>
        {!task && (
          <p className='text-muted-foreground text-sm'>
            O enquadramento foi confirmado. O analista responsável abre o processo no SIGADOC:
            confere o Termo de Abertura e informa o PIN uma única vez; o sistema assina e inclui o
            relatório e os documentos do projeto.
          </p>
        )}
        {running && task && (
          <div className='space-y-2' aria-live='polite'>
            <p className='text-sm font-medium'>
              Montando o processo: {task.progressDone} de {task.progressTotal} itens
            </p>
            <Progress value={pct} />
          </div>
        )}
        {interrupted && task && (
          <Alert variant='destructive'>
            <Icons.alertCircle aria-hidden />
            <AlertTitle>A montagem foi interrompida</AlertTitle>
            <AlertDescription>
              {task.error} O que já foi feito é mantido; informe o PIN de novo para retomar.
            </AlertDescription>
          </Alert>
        )}
        {task?.status === 'cancelled' && (
          <Alert>
            <Icons.alertCircle aria-hidden />
            <AlertTitle>Montagem cancelada</AlertTitle>
            <AlertDescription>{task.error}</AlertDescription>
          </Alert>
        )}
        {task && task.items.length > 0 && (
          <ul className='divide-y'>
            {task.items.map((it, i) => (
              <ItemRow key={`${it.kind}-${i}`} it={it} />
            ))}
          </ul>
        )}
        {responsible ? (
          <div className='flex flex-wrap items-center gap-2'>
            {view.opening.available && (
              <Button onClick={() => setDialog('open')}>Abrir no SIGADOC</Button>
            )}
            {interrupted && <Button onClick={() => setDialog('resume')}>Retomar</Button>}
            {running && <CancelButton project={project} />}
            {!view.opening.available && !running && !interrupted && view.opening.reason && (
              <p className='text-muted-foreground text-sm'>{view.opening.reason}</p>
            )}
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            Só o analista responsável abre o processo. Para continuar, assuma o projeto.
          </p>
        )}
        {dialog === 'open' && <OpenDialog project={project} onClose={() => setDialog(null)} />}
        {dialog === 'resume' && <ResumeDialog project={project} onClose={() => setDialog(null)} />}
      </CardContent>
    </Card>
  );
}
