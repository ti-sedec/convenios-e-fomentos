'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { LoadingButton } from '@/components/ui/loading-button';
import { Textarea } from '@/components/ui/textarea';
import { departmentsQueryOptions } from '@/features/departments/api/queries';
import { projectKeys } from '../../api/queries';
import { assignDepartment, requestComplement } from '../../api/service';
import type { Project, ReviewItem } from '../../api/types';
import { reportStaffError } from './errors';

/** O botão que abre um diálogo: cada diálogo renderiza o seu (sem funções de renderização, que o React recria a cada passo). */
export interface OpenButton {
  label: string;
  variant?: 'default' | 'outline' | 'destructive';
  disabled?: boolean;
  /** Dica do botão desabilitado: diz o que falta. */
  title?: string;
}

function OpenDialogButton({ button, onOpen }: { button: OpenButton; onOpen: () => void }) {
  return (
    <Button
      variant={button.variant ?? 'default'}
      disabled={button.disabled}
      title={button.title}
      onClick={onOpen}
    >
      {button.label}
    </Button>
  );
}

/** Depois de uma ação da equipe a resposta traz o projeto completo: atualiza o detalhe e as filas. */
function useAfterAction(project: Project) {
  const qc = useQueryClient();
  return {
    saved: async (saved: Project) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      await qc.invalidateQueries({ queryKey: projectKeys.lists() });
    },
    failed: async (err: unknown) => {
      if (reportStaffError(err).reload)
        await qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
    }
  };
}

/**
 * Diálogo de uma ação com texto obrigatório (arquivar, negar, recusar): o motivo ou a justificativa, de `min` a `max` caracteres.
 * `button` descreve o botão que o abre.
 */
export function NoteDialog({
  project,
  button,
  title,
  description,
  warning,
  label,
  confirmLabel,
  success,
  min = 3,
  max = 1000,
  destructive = false,
  submit
}: {
  project: Project;
  button: OpenButton;
  title: string;
  description: string;
  /** Aviso em destaque sobre o efeito da ação. */
  warning?: string;
  label: string;
  confirmLabel: string;
  success: string;
  min?: number;
  max?: number;
  destructive?: boolean;
  submit: (note: string) => Promise<Project>;
}) {
  const after = useAfterAction(project);
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState('');
  const text = note.trim();
  const valid = text.length >= min && text.length <= max;
  const run = useMutation({
    mutationFn: () => submit(text),
    onSuccess: async (saved) => {
      await after.saved(saved);
      toast.success(success);
      setOpen(false);
      setNote('');
    },
    onError: async (err) => {
      await after.failed(err);
      setOpen(false);
    }
  });
  return (
    <>
      <OpenDialogButton button={button} onOpen={() => setOpen(true)} />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          {warning && (
            <Alert variant='destructive'>
              <AlertDescription>{warning}</AlertDescription>
            </Alert>
          )}
          <div className='space-y-2'>
            <Label htmlFor='staff-note'>{label}</Label>
            <Textarea
              id='staff-note'
              value={note}
              onChange={(e) => setNote(e.target.value)}
              rows={5}
              maxLength={max}
              aria-describedby='staff-note-hint'
            />
            <p id='staff-note-hint' className='text-muted-foreground text-xs'>
              De {min} a {max.toLocaleString('pt-BR')} caracteres ({text.length} digitados).
            </p>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setOpen(false)}>
              Voltar
            </Button>
            <LoadingButton
              variant={destructive ? 'destructive' : 'default'}
              loading={run.isPending}
              disabled={!valid}
              onClick={() => run.mutate()}
            >
              {confirmLabel}
            </LoadingButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

const MAX_MESSAGE = 2000;
const key = (i: ReviewItem) => `${i.kind}:${i.target}`;
const MAX_ITEM_NOTE = 500;

/** Devolve ao proponente os itens escolhidos (seções, documentos e documentos adicionais), com uma observação por item. */
export function ComplementDialog({ project, button }: { project: Project; button: OpenButton }) {
  const after = useAfterAction(project);
  const [open, setOpen] = useState(false);
  const [message, setMessage] = useState('');
  // alvo -> observação; presença = item escolhido
  const [chosen, setChosen] = useState<Record<string, string>>({});
  const candidates = project.review?.items ?? [];
  const selected = candidates.filter((i) => key(i) in chosen);
  // Os itens reprovados na conferência já vêm marcados, com a justificativa como observação ao proponente.
  const openDialog = () => {
    setChosen((prev) => {
      const next = { ...prev };
      for (const i of candidates)
        if (i.status === 'rejected' && !(key(i) in next)) next[key(i)] = i.note ?? '';
      return next;
    });
    setOpen(true);
  };

  const run = useMutation({
    mutationFn: () =>
      requestComplement(project.id, {
        message: message.trim(),
        items: selected.map((i) => ({
          kind: i.kind,
          target: i.target,
          note: chosen[key(i)].trim()
        }))
      }),
    onSuccess: async (saved) => {
      await after.saved(saved);
      toast.success('Complementação pedida. O proponente foi avisado por e-mail.');
      setOpen(false);
      setMessage('');
      setChosen({});
    },
    onError: async (err) => {
      await after.failed(err);
    }
  });

  return (
    <>
      <OpenDialogButton button={button} onOpen={openDialog} />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>Devolver para complementação</DialogTitle>
            <DialogDescription>
              Escolha o que o proponente deve corrigir. Ele terá 3 dias corridos e só poderá alterar
              os itens devolvidos; se o prazo vencer, o projeto é arquivado.
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-2'>
            <Label htmlFor='complement-message'>Mensagem ao proponente (opcional)</Label>
            <Textarea
              id='complement-message'
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              rows={3}
              maxLength={MAX_MESSAGE}
            />
          </div>
          <fieldset className='space-y-2'>
            <legend className='text-sm font-medium'>
              Itens devolvidos ({selected.length} escolhidos)
            </legend>
            <ul className='space-y-2'>
              {candidates.map((i) => {
                const on = key(i) in chosen;
                return (
                  <li key={key(i)} className='space-y-2 rounded-md border p-2'>
                    <div className='flex items-center gap-2'>
                      <Checkbox
                        id={`ret-${key(i)}`}
                        checked={on}
                        onCheckedChange={(checked) =>
                          setChosen((prev) => {
                            const next = { ...prev };
                            if (checked) next[key(i)] = prev[key(i)] ?? '';
                            else delete next[key(i)];
                            return next;
                          })
                        }
                      />
                      <Label htmlFor={`ret-${key(i)}`} className='font-normal'>
                        {i.label}
                        {i.blocking && (
                          <span className='text-destructive'> (obrigatório, vazio)</span>
                        )}
                      </Label>
                    </div>
                    {on && (
                      <Input
                        value={chosen[key(i)]}
                        onChange={(e) =>
                          setChosen((prev) => ({ ...prev, [key(i)]: e.target.value }))
                        }
                        placeholder='Ex.: o CEP não confere com o comprovante…'
                        autoComplete='off'
                        aria-label={`Observação sobre ${i.label}`}
                        maxLength={MAX_ITEM_NOTE}
                      />
                    )}
                  </li>
                );
              })}
            </ul>
          </fieldset>
          <DialogFooter>
            <Button variant='outline' onClick={() => setOpen(false)}>
              Voltar
            </Button>
            <LoadingButton
              loading={run.isPending}
              disabled={selected.length === 0}
              onClick={() => run.mutate()}
            >
              Devolver ao proponente
            </LoadingButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** Indica o departamento de enquadramento: só áreas finalísticas, e as sem Revisor ativo não podem ser escolhidas. */
export function AssignDepartmentDialog({
  project,
  button
}: {
  project: Project;
  button: OpenButton;
}) {
  const after = useAfterAction(project);
  const [open, setOpen] = useState(false);
  const [departmentId, setDepartmentId] = useState('');
  const departments = useQuery({ ...departmentsQueryOptions(true), enabled: open });
  const lastRefusal = project.framings?.findLast((f) => f.decision === 'refused');

  const run = useMutation({
    mutationFn: () => assignDepartment(project.id, departmentId),
    onSuccess: async (saved) => {
      await after.saved(saved);
      toast.success('Departamento indicado. O Revisor do Departamento foi acionado.');
      setOpen(false);
      setDepartmentId('');
    },
    onError: async (err) => {
      await after.failed(err);
    }
  });
  const list = departments.data ?? [];

  return (
    <>
      <OpenDialogButton button={button} onOpen={() => setOpen(true)} />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Indicar departamento</DialogTitle>
            <DialogDescription>
              O projeto passa a aparecer só para os Revisores do departamento escolhido, que
              confirmam ou recusam o enquadramento. Os Supervisores recebem um e-mail.
            </DialogDescription>
          </DialogHeader>
          {lastRefusal && (
            <Alert>
              <AlertDescription>
                <p className='font-medium'>
                  Última recusa: {lastRefusal.department.name} (indicação nº {lastRefusal.round})
                </p>
                {lastRefusal.justification && (
                  <p className='mt-1 whitespace-pre-line'>{lastRefusal.justification}</p>
                )}
              </AlertDescription>
            </Alert>
          )}
          <fieldset className='space-y-2'>
            <legend className='text-sm font-medium'>Departamento (área finalística)</legend>
            {departments.isLoading && <p className='text-muted-foreground text-sm'>Carregando…</p>}
            {!departments.isLoading && list.length === 0 && (
              <p className='text-muted-foreground text-sm'>
                Nenhum departamento está marcado como área finalística. Peça a quem administra os
                departamentos para marcá-los.
              </p>
            )}
            <RadioGroup value={departmentId} onValueChange={(v) => setDepartmentId(String(v))}>
              {list.map((d) => {
                const unavailable = d.reviewerCount === 0;
                return (
                  <div key={d.id} className='flex items-start gap-2 rounded-md border p-2'>
                    <RadioGroupItem
                      id={`dep-${d.id}`}
                      value={d.id}
                      disabled={unavailable}
                      className='mt-0.5'
                    />
                    <Label
                      htmlFor={`dep-${d.id}`}
                      className='flex flex-col items-start font-normal'
                    >
                      <span className={unavailable ? 'text-muted-foreground' : 'font-medium'}>
                        {d.name}
                      </span>
                      <span className='text-muted-foreground text-xs'>
                        {unavailable
                          ? 'Sem Revisor ativo: não pode receber projetos.'
                          : `${d.reviewerCount} Revisor${d.reviewerCount > 1 ? 'es' : ''} ativo${d.reviewerCount > 1 ? 's' : ''}`}
                      </span>
                    </Label>
                  </div>
                );
              })}
            </RadioGroup>
          </fieldset>
          <DialogFooter>
            <Button variant='outline' onClick={() => setOpen(false)}>
              Voltar
            </Button>
            <LoadingButton
              loading={run.isPending}
              disabled={!departmentId}
              onClick={() => run.mutate()}
            >
              Indicar departamento
            </LoadingButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

/** Confirmação simples de uma ação sem texto (por exemplo, confirmar o enquadramento). */
export function ConfirmDialog({
  project,
  button,
  title,
  description,
  confirmLabel,
  success,
  submit
}: {
  project: Project;
  button: OpenButton;
  title: string;
  description: string;
  confirmLabel: string;
  success: string;
  submit: () => Promise<Project>;
}) {
  const after = useAfterAction(project);
  const [open, setOpen] = useState(false);
  const run = useMutation({
    mutationFn: submit,
    onSuccess: async (saved) => {
      await after.saved(saved);
      toast.success(success);
      setOpen(false);
    },
    onError: async (err) => {
      await after.failed(err);
      setOpen(false);
    }
  });
  return (
    <>
      <OpenDialogButton button={button} onOpen={() => setOpen(true)} />
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant='outline' onClick={() => setOpen(false)}>
              Voltar
            </Button>
            <LoadingButton loading={run.isPending} onClick={() => run.mutate()}>
              {confirmLabel}
            </LoadingButton>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
