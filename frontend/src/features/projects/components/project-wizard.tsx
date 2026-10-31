'use client';

import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useState } from 'react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { AlertModal } from '@/components/modal/alert-modal';
import { QueryError } from '@/components/query-error';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { useCan } from '@/features/auth/session-provider';
import { cn } from '@/lib/utils';
import { accessOf } from '../access';
import { projectKeys, projectQueryOptions } from '../api/queries';
import {
  accessLabels,
  instrumentLabels,
  projectActions,
  sectionLabels,
  statusLabels,
  type ComplementItem,
  type Project,
  type SectionKey
} from '../api/types';
import { LifecycleBanner } from './complement-banner';
import { AccessButton } from './project-access';
import { DocumentsStep } from './documents-step';
import { FundingStep } from './funding-step';
import { InstitutionStep } from './institution-step';
import { IssuesPanel } from './issues-panel';
import { ProponentStep } from './proponent-step';
import { ProposalStep } from './proposal-step';
import { DuplicateButton, WithdrawButton } from './project-actions';
import { ReviewStep } from './review-step';
import { StaffProjectView } from './staff/staff-project';

const steps: SectionKey[] = [
  'proponent',
  'institution',
  'proposal',
  'funding',
  'documents',
  'submission'
];

/** Selo da aba: o que a equipe devolveu nela e, quando já foi tudo alterado, que está pronto para o reenvio. */
function ReturnedBadge({ items }: { items: ComplementItem[] }) {
  if (items.length === 0) return null;
  const done = items.every((i) => i.answered);
  return (
    <Badge variant='outline' className='border-amber-500/60'>
      {done ? 'Alterado' : 'Devolvido'}
    </Badge>
  );
}

export function ProjectWizard({ id }: { id: string }) {
  const can = useCan();
  const qc = useQueryClient();
  const query = useQuery(projectQueryOptions(id));
  const [chosen, setStep] = useState<SectionKey | null>(null);
  const [dirty, setDirty] = useState(false);
  const [pending, setPending] = useState<{ step: SectionKey; focus?: string } | null>(null);
  // Elemento da aba Documentos a que o proponente foi levado pelo aviso de correções.
  const [focus, setFocus] = useState<string | undefined>();
  const onDirty = useCallback((d: boolean) => setDirty(d), []);
  // Relógio da tela (prazo da complementação), fixado na montagem: a leitura da hora não pode acontecer a cada renderização.
  const [now] = useState(() => Date.now());

  if (query.isError) return <QueryError error={query.error} what='o projeto' />;
  if (query.isLoading || !query.data) return <Skeleton className='h-96 w-full' />;
  const project = query.data;
  // Quem não é titular nem membro (a equipe) vê o projeto por outra tela: somente leitura, com a conferência e as decisões.
  if (project.access === '') return <StaffProjectView project={project} />;

  // Titular e convidado com edição alteram; convidado só com leitura apenas vê.
  const base =
    (project.access === 'owner' || project.access === 'edit') && can(projectActions.update);
  const access = accessOf(project, base, now);
  // Projeto já enviado abre na tela do envio (código e comprovante); o rascunho, na primeira etapa; com correções pedidas, na primeira
  // etapa devolvida, que é onde há o que fazer.
  const firstReturned = steps.find((s) => access.returnedIn(s).length > 0);
  const step: SectionKey =
    chosen ?? (project.status === 'draft' ? 'proponent' : (firstReturned ?? 'submission'));
  const editable = access.draft;
  // Nos documentos, a pendência do servidor é uma só ("faltam N"); o selo da aba mostra os N.
  const blocksIn = (s: SectionKey) =>
    project.issues.filter(
      (i) => i.section === s && i.severity === 'block' && i.code !== 'documents_missing'
    ).length + (s === 'documents' ? project.documents.requiredMissing : 0);

  // Trocar de etapa com alterações não salvas pede confirmação, para não perder o que foi digitado.
  const go = (s: SectionKey, to?: string) => {
    if (s === step) return setFocus(to);
    if (dirty) setPending({ step: s, focus: to });
    else {
      setFocus(to);
      setStep(s);
    }
  };
  const onSaved = (saved: Project, advance: boolean) => {
    setDirty(false);
    if (advance) setStep(steps[Math.min(steps.indexOf(step) + 1, steps.length - 1)]);
    qc.setQueryData(projectKeys.detail(id), saved);
  };
  const common = { project, onSaved, onDirty };
  const readOnly = (s: SectionKey) => !access.step(s);

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <h2 className='text-lg font-semibold'>{project.proposal.title || 'Projeto sem título'}</h2>
        <Badge variant='secondary'>{statusLabels[project.status]}</Badge>
        {project.institution.instrument && (
          <Badge variant='outline'>{instrumentLabels[project.institution.instrument]}</Badge>
        )}
        {project.access && project.access !== 'owner' && (
          <Badge variant='outline'>Compartilhado com você · {accessLabels[project.access]}</Badge>
        )}
        {project.code && (
          <span className='text-muted-foreground font-mono text-sm'>{project.code}</span>
        )}
        <span className='ml-auto flex gap-2'>
          <AccessButton project={project} />
          <DuplicateButton project={project} />
          <WithdrawButton project={project} />
        </span>
      </div>
      {project.originProjectId && (
        <p className='text-muted-foreground text-sm'>
          Criado a partir de outro projeto{project.originCode ? ` (${project.originCode})` : ''}.
        </p>
      )}
      <LifecycleBanner project={project} now={now} onGo={go} />

      {project.status === 'draft' && !editable && (
        <Alert>
          <AlertDescription>
            {project.access === 'read'
              ? 'Você tem acesso de leitura a este projeto: pode ver os dados e os documentos, mas não alterá-los.'
              : 'Você pode ver este projeto, mas só quem o criou pode editá-lo.'}
          </AlertDescription>
        </Alert>
      )}

      <nav aria-label='Etapas do cadastro' className='flex flex-wrap gap-1 border-b'>
        {steps.map((s, idx) => (
          <button
            key={s}
            type='button'
            onClick={() => go(s)}
            aria-current={s === step ? 'step' : undefined}
            className={cn(
              '-mb-px flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium transition-colors',
              s === step
                ? 'border-primary text-foreground'
                : 'text-muted-foreground hover:text-foreground border-transparent'
            )}
          >
            <span className='bg-muted flex size-5 items-center justify-center rounded-full text-xs'>
              {idx + 1}
            </span>
            {sectionLabels[s]}
            <ReturnedBadge items={access.returnedIn(s)} />
            {blocksIn(s) > 0 && (
              <Badge variant='secondary' aria-label={`${blocksIn(s)} pendências`}>
                {blocksIn(s)}
              </Badge>
            )}
          </button>
        ))}
      </nav>

      <div className='grid gap-6 lg:grid-cols-[1fr_20rem]'>
        <div className='min-w-0 max-w-3xl'>
          {access.complement !== null && step !== 'submission' && readOnly(step) && (
            <Alert className='mb-4'>
              <AlertDescription>
                A equipe não devolveu esta etapa: ela fica só para consulta. Corrija as etapas
                marcadas como devolvidas.
              </AlertDescription>
            </Alert>
          )}
          {/* A chave remonta o formulário com os valores já normalizados pelo servidor depois de cada gravação. */}
          {step === 'proponent' && (
            <ProponentStep key={project.version} {...common} readOnly={readOnly('proponent')} />
          )}
          {step === 'institution' && (
            <InstitutionStep key={project.version} {...common} readOnly={readOnly('institution')} />
          )}
          {step === 'proposal' && (
            <ProposalStep key={project.version} {...common} readOnly={readOnly('proposal')} />
          )}
          {step === 'funding' && (
            <FundingStep key={project.version} {...common} readOnly={readOnly('funding')} />
          )}
          {step === 'documents' && (
            <DocumentsStep
              project={project}
              access={access}
              focus={focus}
              onContinue={() => go('submission')}
            />
          )}
          {step === 'submission' && (
            <ReviewStep
              project={project}
              editable={editable || access.complement !== null}
              access={access}
              onGo={go}
            />
          )}
          <p className='text-muted-foreground mt-6 text-xs'>Cada etapa é salva separadamente.</p>
        </div>
        {(project.status === 'draft' || access.complement !== null) && (
          <aside aria-label='Pendências do projeto'>
            <IssuesPanel issues={project.issues} onGo={go} />
          </aside>
        )}
      </div>

      <AlertModal
        isOpen={pending !== null}
        onClose={() => setPending(null)}
        onConfirm={() => {
          if (pending) {
            setFocus(pending.focus);
            setStep(pending.step);
          }
          setDirty(false);
          setPending(null);
        }}
        loading={false}
        title='Descartar as alterações desta etapa?'
        description='Você alterou campos que ainda não foram salvos. Se continuar, essas alterações serão perdidas.'
        confirmLabel='Descartar e continuar'
        cancelLabel='Voltar e salvar'
      />
    </div>
  );
}
