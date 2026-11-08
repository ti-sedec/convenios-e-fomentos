'use client';

import { parseAsString, useQueryState } from 'nuqs';
import { Badge } from '@/components/ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useCan } from '@/features/auth/session-provider';
import {
  framingStatusLabels,
  instrumentLabels,
  projectActions,
  stageLabels,
  statusLabels,
  type Project
} from '../../api/types';
import { ProjectFull, sectionAnchor } from '../project-view/project-full';
import { ProjectSummary } from '../project-view/project-summary';
import { staffViewModeOf } from '../project-view/view-mode';
import { FramingPanel } from './framing-panel';
import { HistoryPanel } from './history-panel';
import { ReviewPanel } from './review-panel';
import { StaffActions } from './staff-actions';
import { ResponsibleNotice, StaffBanner } from './staff-banner';

interface TabSpec {
  key: string;
  label: string;
}

/**
 * O projeto visto pela equipe (quem não é titular nem membro): cabeçalho com a situação, o banner e as ações, e as abas. O Revisor do
 * Departamento abre no resumo enxuto; os demais perfis, na conferência ou na visualização completa (as duas são de leitura, não são o
 * formulário do cadastro). A visualização completa fica a uma aba de distância para todos.
 */
export function StaffProjectView({ project }: { project: Project }) {
  const can = useCan();
  const [tab, setTab] = useQueryState('aba', parseAsString.withDefault(''));
  const mode = staffViewModeOf(can);

  const tabs: TabSpec[] = [
    ...(mode === 'summary' ? [{ key: 'resumo', label: 'Resumo' }] : []),
    ...(mode === 'full' && project.review ? [{ key: 'conferencia', label: 'Conferência' }] : []),
    ...(project.status === 'draft' ? [] : [{ key: 'enquadramento', label: 'Enquadramento' }]),
    { key: 'completo', label: 'Projeto completo' },
    { key: 'historico', label: 'Histórico' }
  ];
  // Cada perfil começa onde trabalha: o Revisor no resumo, o Analista na conferência.
  let first = 'completo';
  if (mode === 'summary') first = 'resumo';
  else if (project.review && can(projectActions.reviewItem)) first = 'conferencia';
  else if (project.framing && can(projectActions.reviewFraming)) first = 'enquadramento';
  const current = tabs.find((t) => t.key === tab) ?? tabs.find((t) => t.key === first) ?? tabs[0];
  const pending = project.review?.summary.pending ?? 0;

  // "Ver dados" da conferência aponta uma seção do cadastro: abre a visualização completa e rola até ela.
  const goSection = (section: string) => {
    void setTab('completo').then(() =>
      requestAnimationFrame(() =>
        document.getElementById(sectionAnchor(section))?.scrollIntoView({ block: 'start' })
      )
    );
  };

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <h2 className='text-lg font-semibold'>{project.proposal.title || 'Projeto sem título'}</h2>
        <Badge variant={project.status === 'archived' ? 'outline' : 'secondary'}>
          {statusLabels[project.status]}
        </Badge>
        {project.institution.instrument && (
          <Badge variant='outline'>{instrumentLabels[project.institution.instrument]}</Badge>
        )}
        {project.status === 'submitted' && project.stage && (
          <Badge variant='outline'>{stageLabels[project.stage]}</Badge>
        )}
        {project.framing && (
          <Badge variant='outline'>
            {
              framingStatusLabels[
                project.framing.decision === 'confirmed' ? 'confirmed' : 'pending'
              ]
            }
          </Badge>
        )}
        {project.code && (
          <span className='text-muted-foreground font-mono text-sm'>{project.code}</span>
        )}
      </div>
      <p className='text-muted-foreground text-sm'>
        {project.institution.name || 'Instituição não informada'}
        {project.framing && <> · Departamento: {project.framing.department.name}</>}
        {project.analyst && (
          <> · Analista: {project.analyst.isMe ? 'você' : project.analyst.name}</>
        )}
      </p>
      <ResponsibleNotice project={project} />
      <StaffBanner project={project} />
      <StaffActions project={project} />

      <Tabs value={current.key} onValueChange={(v) => void setTab(v)}>
        <TabsList className='h-auto flex-wrap justify-start'>
          {tabs.map((t) => (
            <TabsTrigger key={t.key} value={t.key}>
              {t.label}
              {t.key === 'conferencia' && pending > 0 && (
                <Badge
                  variant='secondary'
                  className='ml-1 tabular-nums'
                  aria-label={`${pending} pendentes`}
                >
                  {pending}
                </Badge>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value='resumo'>
          <ProjectSummary project={project} />
        </TabsContent>
        <TabsContent value='conferencia'>
          <ReviewPanel project={project} onGoTab={goSection} />
        </TabsContent>
        <TabsContent value='enquadramento'>
          <FramingPanel project={project} />
        </TabsContent>
        <TabsContent value='completo'>
          <ProjectFull project={project} />
        </TabsContent>
        <TabsContent value='historico'>
          <HistoryPanel project={project} />
        </TabsContent>
      </Tabs>
    </div>
  );
}
