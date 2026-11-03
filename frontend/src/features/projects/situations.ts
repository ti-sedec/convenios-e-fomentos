import type { OverviewCounts } from '@/features/proponent-overview/api/types';
import type { ListProjectsParams } from './api/service';

/**
 * Uma situação do projeto, do ponto de vista do proponente: o rótulo, o filtro da listagem e de onde sai a contagem. Cada projeto cai
 * em uma só. O enquadramento é interno, então continua em "Com a equipe". `key` é o valor de `?situacao=` na URL.
 */
export interface Situation {
  key: string;
  label: string;
  params: Pick<ListProjectsParams, 'status' | 'stage'>;
  count: (c: OverviewCounts) => number;
}

export const situations: Situation[] = [
  { key: 'rascunhos', label: 'Rascunhos', params: { status: 'draft' }, count: (c) => c.draft },
  {
    key: 'com-a-equipe',
    label: 'Com a equipe',
    params: { status: 'submitted', stage: ['awaiting_checklist', 'in_analysis'] },
    count: (c) => c.awaiting_checklist + c.in_analysis
  },
  {
    key: 'correcoes',
    label: 'Aguardam suas correções',
    params: { status: 'submitted', stage: ['awaiting_proponent'] },
    count: (c) => c.awaiting_proponent
  },
  {
    key: 'parecer',
    label: 'Aguardando parecer',
    params: { status: 'submitted', stage: ['awaiting_opinion'] },
    count: (c) => c.awaiting_opinion
  },
  {
    key: 'arquivados',
    label: 'Arquivados',
    params: { status: 'archived' },
    count: (c) => c.archived
  }
];

export const situationByKey = (key: string) => situations.find((s) => s.key === key);
