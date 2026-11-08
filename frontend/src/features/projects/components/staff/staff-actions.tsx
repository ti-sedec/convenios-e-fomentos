'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { LoadingButton } from '@/components/ui/loading-button';
import { useCan } from '@/features/auth/session-provider';
import { projectKeys } from '../../api/queries';
import {
  archiveProject,
  confirmFraming,
  denyProject,
  refuseFraming,
  startAnalysis,
  takeOverAnalysis
} from '../../api/service';
import { projectActions, type Project } from '../../api/types';
import { AssignDepartmentDialog, ComplementDialog, ConfirmDialog, NoteDialog } from './dialogs';
import { reportStaffError } from './errors';
import { isResponsible } from './review-panel';

function StartAnalysisButton({ project }: { project: Project }) {
  const qc = useQueryClient();
  const run = useMutation({
    mutationFn: () => startAnalysis(project.id),
    onSuccess: async (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      await qc.invalidateQueries({ queryKey: projectKeys.lists() });
      toast.success('Análise iniciada. Você é o analista responsável por este projeto.');
    },
    onError: async (err) => {
      if (reportStaffError(err).reload)
        await qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
    }
  });
  return (
    <LoadingButton loading={run.isPending} onClick={() => run.mutate()}>
      <Icons.circleCheck aria-hidden /> Iniciar análise
    </LoadingButton>
  );
}

/** Ações do Analista, só as que cabem na etapa e na conferência; o servidor decide e a mensagem do erro explica. */
function AnalystActions({ project }: { project: Project }) {
  const can = useCan();
  if (project.status !== 'submitted') return null;
  const stage = project.stage;
  const framingPending = project.framing?.decision === 'pending';
  const working = stage === 'awaiting_checklist' || stage === 'in_analysis';
  const review = project.review;
  // Só o responsável devolve, arquiva e indica o departamento; os outros Analistas assumem o projeto para continuar.
  const responsible = isResponsible(project);
  const canTakeOver =
    can(projectActions.startAnalysis) &&
    !responsible &&
    (stage === 'in_analysis' || stage === 'awaiting_proponent') &&
    !framingPending;

  const showAssign =
    responsible &&
    can(projectActions.assignDepartment) &&
    stage === 'in_analysis' &&
    !framingPending;
  const readyToAssign = !!review?.allApproved;
  const missing = review
    ? `${review.summary.pending} pendente(s) e ${review.summary.blockers} impedimento(s)`
    : '';

  return (
    <>
      {can(projectActions.startAnalysis) && stage === 'awaiting_checklist' && (
        <StartAnalysisButton project={project} />
      )}
      {canTakeOver && (
        <ConfirmDialog
          project={project}
          button={{ label: 'Assumir projeto' }}
          title='Assumir este projeto?'
          description={`O projeto está com ${project.analyst?.name}. Ao assumir, ele passa a ser seu: só você continua a análise, e ${project.analyst?.name} deixa de poder conferir, devolver ou arquivar (continua podendo visualizar) e será avisado por e-mail. A conferência já feita é mantida.`}
          confirmLabel='Assumir projeto'
          success='Você assumiu o projeto e passou a ser o analista responsável.'
          submit={() => takeOverAnalysis(project.id)}
        />
      )}
      {showAssign && (
        <AssignDepartmentDialog
          project={project}
          button={{ label: 'Indicar departamento', disabled: !readyToAssign }}
        />
      )}
      {showAssign && !readyToAssign && (
        <p className='text-muted-foreground w-full text-sm'>
          Para indicar o departamento, aprove todos os itens da conferência ({missing}).
        </p>
      )}
      {responsible && can(projectActions.requestComplement) && working && !framingPending && (
        <ComplementDialog
          project={project}
          button={{ label: 'Devolver para complementação', variant: 'outline' }}
        />
      )}
      {responsible && can(projectActions.archive) && working && !framingPending && (
        <NoteDialog
          project={project}
          title='Arquivar por falta de documentos obrigatórios'
          description='O projeto é arquivado e deixa de andar (Portaria 71, art. 1º, §2º). O proponente, o Revisor do Departamento, o Analista e os Supervisores são avisados por e-mail.'
          warning='O arquivamento é definitivo. Se o objetivo é pedir ajustes, use “Devolver para complementação”.'
          label='Motivo (obrigatório)'
          confirmLabel='Arquivar projeto'
          success='Projeto arquivado.'
          destructive
          submit={(note) => archiveProject(project.id, note)}
          button={{ label: 'Arquivar por falta de documentos', variant: 'outline' }}
        />
      )}
    </>
  );
}

/** Ações do Revisor do Departamento: decidir o enquadramento pendente. `round` é a rodada que ele está vendo. */
function ReviewerActions({ project }: { project: Project }) {
  const can = useCan();
  const framing = project.framing;
  if (!can(projectActions.reviewFraming) || project.status !== 'submitted') return null;
  if (!framing || framing.decision !== 'pending') return null;
  return (
    <>
      <ConfirmDialog
        project={project}
        button={{ label: 'Confirmar enquadramento' }}
        title='Confirmar o enquadramento?'
        description={`O projeto se enquadra no departamento ${framing.department.name} e passa a aguardar o parecer. Vale a primeira decisão de qualquer Revisor do departamento.`}
        confirmLabel='Confirmar enquadramento'
        success='Enquadramento confirmado. O projeto aguarda o parecer.'
        submit={() => confirmFraming(project.id, framing.round)}
      />
      <NoteDialog
        project={project}
        button={{ label: 'Recusar enquadramento', variant: 'outline' }}
        title='Recusar o enquadramento'
        description={`O projeto não se enquadra no departamento ${framing.department.name}. Ele sai da sua fila e volta ao Analista, que indicará outro departamento. Os Supervisores são avisados.`}
        label='Justificativa (obrigatória)'
        confirmLabel='Recusar enquadramento'
        success='Enquadramento recusado. O projeto voltou ao Analista.'
        destructive
        submit={(note) => refuseFraming(project.id, framing.round, note)}
      />
    </>
  );
}

/** Ação do Secretário de Estado: negar o projeto em qualquer etapa. Arquiva na hora e avisa todos. */
function SecretaryActions({ project }: { project: Project }) {
  const can = useCan();
  if (!can(projectActions.deny) || project.status !== 'submitted') return null;
  return (
    <NoteDialog
      project={project}
      button={{ label: 'Negar projeto', variant: 'destructive' }}
      title='Negar o projeto'
      description='A negação vale em qualquer etapa e dispensa o parecer.'
      warning='O projeto será arquivado agora, sem volta. O proponente, o Revisor do Departamento (se já houver), o Analista e os Supervisores serão avisados por e-mail.'
      label='Motivo da negação (obrigatório)'
      confirmLabel='Negar e arquivar'
      success='Projeto negado e arquivado.'
      destructive
      submit={(note) => denyProject(project.id, note)}
    />
  );
}

/** Barra de ações da equipe sobre o projeto, conforme o perfil, a etapa e a situação do enquadramento. */
export function StaffActions({ project }: { project: Project }) {
  return (
    <div className='flex flex-wrap items-center gap-2'>
      <AnalystActions project={project} />
      <ReviewerActions project={project} />
      <SecretaryActions project={project} />
    </div>
  );
}
