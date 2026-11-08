'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Icons } from '@/components/icons';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { LoadingButton } from '@/components/ui/loading-button';
import { Progress } from '@/components/ui/progress';
import { useCan } from '@/features/auth/session-provider';
import { projectKeys } from '../../api/queries';
import { reviewItem } from '../../api/service';
import {
  projectActions,
  reviewKindLabels,
  type Project,
  type ReviewDecision,
  type ReviewFile,
  type ReviewItem,
  type ReviewKind
} from '../../api/types';
import { DocumentReviewDialog, MAX_REJECTION_NOTE } from './document-review-dialog';
import { NoteDialog } from './dialogs';
import { reportStaffError } from './errors';
import { formatDateTimeCuiaba } from '../../format';

/** Aba do cadastro que mostra cada seção da conferência. */
const sectionTab: Record<string, string> = {
  proponent: 'proponente',
  institution: 'instituicao',
  proposal: 'proposta',
  funding: 'recursos',
  'document-options': 'documentos'
};

/** Só o analista responsável continua a análise; sem responsável (caso raro), qualquer Analista. */
export function isResponsible(project: Project): boolean {
  return !project.analyst || project.analyst.isMe;
}

/**
 * O Analista pode aprovar e desfazer quando é o responsável, o projeto está em análise e nenhum enquadramento aguarda decisão.
 * Os outros Analistas só visualizam: para continuar, assumem o projeto.
 */
export function canReviewNow(project: Project, can: (action: string) => boolean): boolean {
  return (
    can(projectActions.reviewItem) &&
    isResponsible(project) &&
    project.status === 'submitted' &&
    project.stage === 'in_analysis' &&
    project.framing?.decision !== 'pending'
  );
}

function FileLine({ file }: { file: ReviewFile }) {
  return (
    <li className='flex min-w-0 items-center gap-2 text-sm'>
      <Icons.paperclip className='text-muted-foreground size-4 shrink-0' aria-hidden />
      <span className='truncate'>{file.filename}</span>
      {file.opened ? (
        <Badge variant='secondary'>Visto</Badge>
      ) : (
        <Badge variant='outline'>Não visto</Badge>
      )}
    </li>
  );
}

function ReviewRow({
  project,
  item,
  canReview,
  onGoTab
}: {
  project: Project;
  item: ReviewItem;
  canReview: boolean;
  onGoTab: (tab: string) => void;
}) {
  const qc = useQueryClient();
  const [viewing, setViewing] = useState(false);
  const approved = item.status === 'approved';
  const rejected = item.status === 'rejected';
  const hasFiles = item.files.length > 0;
  const change = useMutation({
    mutationFn: (decision: ReviewDecision) =>
      reviewItem(project.id, { kind: item.kind, target: item.target, decision }),
    onSuccess: (saved) => qc.setQueryData(projectKeys.detail(project.id), saved),
    onError: async (err) => {
      if (reportStaffError(err).reload)
        await qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
    }
  });
  const tab = item.kind === 'section' ? sectionTab[item.target] : undefined;
  // Item sem arquivo (as seções) se decide aqui; com arquivo, no visualizador, depois de ver cada um.
  const decideHere = canReview && !item.blocking && !hasFiles;
  const canReject = item.kind !== 'extra';

  return (
    <li className='space-y-2 rounded-md border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex min-w-0 items-center gap-2'>
          {approved ? (
            <Icons.circleCheck className='size-4 shrink-0 text-green-600' aria-hidden />
          ) : rejected ? (
            <Icons.circleX className='text-destructive size-4 shrink-0' aria-hidden />
          ) : (
            <Icons.circle className='text-muted-foreground size-4 shrink-0' aria-hidden />
          )}
          <span className='font-medium'>{item.label}</span>
          {item.blocking ? (
            <Badge variant='destructive'>Impedimento</Badge>
          ) : approved ? (
            <Badge variant='secondary'>Aprovado</Badge>
          ) : rejected ? (
            <Badge variant='destructive'>Reprovado</Badge>
          ) : (
            <Badge variant='outline'>Pendente</Badge>
          )}
        </div>
        <div className='flex flex-wrap gap-1'>
          {tab && (
            <Button size='sm' variant='ghost' onClick={() => onGoTab(tab)}>
              Ver dados
            </Button>
          )}
          {hasFiles && !item.blocking && (
            <Button
              size='sm'
              variant={canReview && !approved && !rejected ? 'default' : 'outline'}
              onClick={() => setViewing(true)}
            >
              <Icons.eye aria-hidden /> {canReview ? 'Analisar documento' : 'Ver documento'}
            </Button>
          )}
          {canReview && !item.blocking && (approved || rejected) && (
            <LoadingButton
              size='sm'
              variant='outline'
              loading={change.isPending}
              onClick={() => change.mutate('pending')}
            >
              Desfazer
            </LoadingButton>
          )}
          {decideHere && !rejected && canReject && (
            <NoteDialog
              project={project}
              button={{ label: 'Reprovar', variant: 'outline' }}
              title={`Reprovar: ${item.label}`}
              description='A justificativa fica registrada no item e pode seguir como observação ao devolver para correção.'
              label='Justificativa da reprovação'
              confirmLabel='Reprovar'
              success='Item reprovado.'
              min={3}
              max={MAX_REJECTION_NOTE}
              destructive
              submit={(note) =>
                reviewItem(project.id, {
                  kind: item.kind,
                  target: item.target,
                  decision: 'rejected',
                  note
                })
              }
            />
          )}
          {decideHere && !approved && (
            <LoadingButton
              size='sm'
              loading={change.isPending}
              onClick={() => change.mutate('approved')}
            >
              Aprovar
            </LoadingButton>
          )}
        </div>
      </div>
      {item.blocking && item.reason && (
        <p className='text-sm'>{item.reason} Devolva o item ao proponente ou arquive o projeto.</p>
      )}
      {rejected && item.note && (
        <p className='text-destructive text-sm'>
          <span className='font-medium'>Motivo:</span> {item.note}
        </p>
      )}
      {(approved || rejected) && item.reviewedBy && (
        <p className='text-muted-foreground text-xs'>
          {approved ? 'Aprovado' : 'Reprovado'} por {item.reviewedBy}
          {item.reviewedAt ? ` em ${formatDateTimeCuiaba(item.reviewedAt)}` : ''}
        </p>
      )}
      {hasFiles && (
        <ul className='space-y-1.5 border-t pt-2'>
          {item.files.map((f) => (
            <FileLine key={f.id} file={f} />
          ))}
        </ul>
      )}
      {hasFiles && !item.blocking && (
        <DocumentReviewDialog
          project={project}
          item={item}
          canReview={canReview}
          open={viewing}
          onOpenChange={setViewing}
        />
      )}
    </li>
  );
}

/** Conferência da análise: o Analista abre cada documento e aprova item a item; os demais perfis acompanham. */
export function ReviewPanel({
  project,
  onGoTab
}: {
  project: Project;
  onGoTab: (tab: string) => void;
}) {
  const can = useCan();
  const review = project.review;
  if (!review) return null;
  const canReview = canReviewNow(project, can);
  const { total, approved, rejected, blockers } = review.summary;
  const percent = total ? Math.round((approved / total) * 100) : 0;

  const blocking = review.items.filter((i) => i.blocking);
  const kinds: ReviewKind[] = ['section', 'document', 'extra'];

  let hint = 'Somente leitura.';
  if (canReview) {
    hint =
      'Analise cada documento no visualizador: veja todos os arquivos e aprove ou reprove com justificativa. Aprove todos os itens para poder indicar o departamento.';
  } else if (project.framing?.decision === 'pending') {
    hint = 'A conferência fica travada enquanto o departamento não decide o enquadramento.';
  } else if (project.status === 'submitted' && project.stage === 'awaiting_checklist') {
    hint = 'Inicie a análise para poder aprovar os itens.';
  } else if (project.stage === 'awaiting_proponent') {
    hint = 'O projeto está com o proponente; a conferência volta quando ele reenviar.';
  }

  return (
    <div className='max-w-3xl space-y-4'>
      <Card>
        <CardHeader>
          <CardTitle className='text-base'>Conferência</CardTitle>
          <CardDescription>{hint}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-2'>
          <p className='text-sm font-medium'>
            {approved} de {total} itens aprovados
            {rejected > 0 && (
              <span className='text-destructive'>
                {' '}
                · {rejected} reprovado{rejected > 1 ? 's' : ''}
              </span>
            )}
            {blockers > 0 && (
              <span className='text-destructive'>
                {' '}
                · {blockers} impedimento{blockers > 1 ? 's' : ''}
              </span>
            )}
          </p>
          <Progress value={percent} aria-label='Itens aprovados' />
          {review.allApproved && (
            <p className='text-sm font-medium text-green-700'>
              Tudo aprovado: o departamento já pode ser indicado.
            </p>
          )}
        </CardContent>
      </Card>

      {blocking.length > 0 && (
        <section aria-labelledby='review-blockers' className='space-y-2'>
          <h3 id='review-blockers' className='text-base font-semibold'>
            Impedimentos
          </h3>
          <ul className='space-y-2'>
            {blocking.map((item) => (
              <ReviewRow
                key={`${item.kind}-${item.target}`}
                project={project}
                item={item}
                canReview={canReview}
                onGoTab={onGoTab}
              />
            ))}
          </ul>
        </section>
      )}

      {kinds.map((kind) => {
        const items = review.items.filter((i) => i.kind === kind && !i.blocking);
        if (items.length === 0) return null;
        return (
          <section key={kind} aria-labelledby={`review-${kind}`} className='space-y-2'>
            <h3 id={`review-${kind}`} className='text-base font-semibold'>
              {reviewKindLabels[kind]}
            </h3>
            <ul className='space-y-2'>
              {items.map((item) => (
                <ReviewRow
                  key={`${item.kind}-${item.target}`}
                  project={project}
                  item={item}
                  canReview={canReview}
                  onGoTab={onGoTab}
                />
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}
