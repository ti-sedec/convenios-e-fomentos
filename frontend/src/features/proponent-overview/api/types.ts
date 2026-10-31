/** Contratos de GET /admin/projects/overview e /admin/projects/activity (backend/api/openapi.yaml). */

/** Filtro de acesso da API: tudo o que é da pessoa, só o que ela é titular, ou só onde é membro. */
export type AccessFilter = 'all' | 'owner' | 'member';

/** Um projeto conta em uma só contagem; o enquadramento pendente continua em `in_analysis` e arquivado só em `archived`. */
export interface OverviewCounts {
  draft: number;
  awaiting_checklist: number;
  in_analysis: number;
  awaiting_proponent: number;
  awaiting_opinion: number;
  archived: number;
}

export type AttentionKind = 'complement_open' | 'signature_pending' | 'draft' | 'shared_new';

/** Algo que pede uma ação da pessoa. Os campos usados dependem de `kind`. */
export interface AttentionItem {
  kind: AttentionKind;
  projectId: string;
  title: string;
  code?: string;
  /** complement_open */
  dueAt?: string;
  overdue?: boolean;
  items?: number;
  /** signature_pending */
  declaration?: string;
  /** draft: `blocking` só vem para os rascunhos mais recentes. */
  updatedAt?: string;
  blocking?: number;
  /** shared_new */
  level?: 'read' | 'edit';
  invitedBy?: string;
}

export interface Overview {
  counts: OverviewCounts;
  /** Totais por tipo de acesso, sem o filtro pedido. */
  byAccess: { owner: number; member: number };
  /** Projetos enviados e ativos, sem rascunhos nem arquivados. */
  activeCents: { total: number; convenio: number; fomento: number };
  attention: AttentionItem[];
}

export type ActivityKind =
  | 'submitted'
  | 'analysis_started'
  | 'complement_requested'
  | 'resubmitted'
  | 'awaiting_opinion'
  | 'archived'
  | 'shared_with_you'
  | (string & {});

/** Quem agiu, sem nome: a própria pessoa, o titular ou outro membro, a equipe ou o sistema. */
export type ActivityBy = 'you' | 'member' | 'team' | 'system';

export interface ActivityItem {
  id: string;
  at: string;
  kind: ActivityKind;
  by: ActivityBy;
  projectId: string;
  title: string;
  code?: string;
  data: {
    late?: boolean;
    items?: number;
    dueAt?: string;
    version?: number;
    reason?: string;
    level?: 'read' | 'edit';
  };
}
