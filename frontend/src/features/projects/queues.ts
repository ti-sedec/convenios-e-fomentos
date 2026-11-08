import type { ListProjectsParams } from './api/service';
import { projectActions } from './api/types';

/** Uma aba da fila da equipe: o rótulo e os filtros da listagem (o servidor ainda limita pela permissão do usuário). */
export interface QueueTab {
  key: string;
  label: string;
  params: ListProjectsParams;
}

// Os filtros por etapa só valem para projetos ativos: um projeto arquivado guarda a etapa em que estava.
const active = { status: 'submitted' } as const;

const analystTabs: QueueTab[] = [
  {
    key: 'aguardando',
    label: 'Aguardando análise',
    params: { ...active, stage: ['awaiting_checklist'] }
  },
  {
    key: 'analise',
    label: 'Em análise',
    params: { ...active, stage: ['in_analysis'], framing: 'none' }
  },
  { key: 'enquadramento', label: 'Em enquadramento', params: { ...active, framing: 'pending' } },
  {
    key: 'proponente',
    label: 'Aguardando proponente',
    params: { ...active, stage: ['awaiting_proponent'] }
  },
  {
    key: 'parecer',
    label: 'Aguardando parecer',
    params: { ...active, stage: ['awaiting_opinion'] }
  },
  { key: 'arquivados', label: 'Arquivados', params: { status: 'archived' } },
  { key: 'todos', label: 'Todos', params: {} }
];

const reviewerTabs: QueueTab[] = [
  { key: 'pendentes', label: 'Pendentes de decisão', params: { ...active, framing: 'pending' } },
  { key: 'confirmados', label: 'Confirmados', params: { ...active, framing: 'confirmed' } },
  { key: 'arquivados', label: 'Arquivados', params: { status: 'archived' } }
];

const generalTabs: QueueTab[] = [
  { key: 'todos', label: 'Todos', params: {} },
  { key: 'analise', label: 'Em análise', params: { ...active, stage: ['in_analysis'] } },
  {
    key: 'parecer',
    label: 'Aguardando parecer',
    params: { ...active, stage: ['awaiting_opinion'] }
  },
  { key: 'arquivados', label: 'Arquivados', params: { status: 'archived' } }
];

/**
 * As abas de cada perfil, pela ação que o define: o Analista assume a análise; o Revisor decide o enquadramento; o Secretário e o
 * Supervisor só acompanham. O Super Admin (passa em qualquer ação) fica com as do Analista.
 */
export function queueTabsFor(can: (action: string) => boolean): QueueTab[] {
  if (can(projectActions.startAnalysis)) return analystTabs;
  if (can(projectActions.reviewFraming)) return reviewerTabs;
  return generalTabs;
}
