/** Contratos de GET /admin/projects/queue/overview (backend/api/openapi.yaml). */

/** `team`: tudo o que a pessoa enxerga; `mine`: só os projetos cuja análise ela assumiu. */
export type QueueScope = 'team' | 'mine';

/** Um projeto conta em uma só etapa; o enquadramento pendente conta só em `framing_pending`. */
export interface QueueCounts {
  awaiting_checklist: number;
  in_analysis: number;
  framing_pending: number;
  awaiting_proponent: number;
  awaiting_opinion: number;
  /** Arquivados nos últimos 30 dias. */
  archived_30d: number;
}

/** Dias vigentes de "parado" por etapa de trabalho. Zero = sem regra vigente. */
export interface StalledLimits {
  awaiting_checklist: number;
  in_analysis: number;
  framing: number;
  awaiting_opinion: number;
}

/** Etapa de trabalho: o estado do projeto visto de quem está com ele. */
export type WorkStage = 'awaiting_checklist' | 'in_analysis' | 'framing' | 'awaiting_opinion';
export type Holder = 'team' | 'analyst' | 'department';

export interface QueueItem {
  projectId: string;
  title: string;
  code?: string;
  institution?: string;
  /** Há quanto tempo o projeto está com o responsável atual (horas corridas). */
  sinceSeconds: number;
  /** Enviado fora do prazo eleitoral. */
  late?: boolean;
  /** Fila de entrada: passou do limite da etapa. */
  overLimit?: boolean;
  stage?: WorkStage;
  /** Só no bloco "parados". */
  holder?: Holder;
  limitDays?: number;
  analyst?: { id: string; name: string };
  /** Só no bloco "recusados". */
  department?: string;
  refusedAt?: string;
  /** Só no bloco "prazos". */
  dueAt?: string;
  overdue?: boolean;
}

export interface QueueBlock {
  total: number;
  items: QueueItem[];
  /** Só no "pronto para indicar": havia mais candidatos que o limite examinado; o total é "N ou mais". */
  capped?: boolean;
}

export type BlockKey = 'refused' | 'resubmitted' | 'entry' | 'ready' | 'stalled' | 'deadlines';

export interface QueueOverview {
  counts: QueueCounts;
  limits: StalledLimits;
  blocks: Record<BlockKey, QueueBlock>;
}

/** Contratos de GET /admin/projects/queue/activity e /queue/me. */

export type QueueActivityKind =
  | 'submitted'
  | 'resubmitted'
  | 'analysis_started'
  | 'complement_requested'
  | 'department_assigned'
  | 'framing_confirmed'
  | 'framing_refused'
  | 'archived'
  | (string & {});

/** Quem agiu: a equipe vem com nome; o proponente e o sistema, só como tipo. `self` é a própria pessoa. */
export interface QueueActor {
  type: 'admin' | 'proponent' | 'system';
  name?: string;
  self?: boolean;
}

export interface QueueActivityItem {
  id: string;
  at: string;
  kind: QueueActivityKind;
  by: QueueActor;
  projectId: string;
  title: string;
  code?: string;
  data: {
    late?: boolean;
    version?: number;
    items?: number;
    dueAt?: string;
    department?: string;
    reason?: string;
  };
}

export interface MyCounts {
  assumed: number;
  complements: number;
  indicated: number;
  archived: number;
}

/** O que a própria pessoa fez nos últimos 7 e 30 dias. */
export interface MySummary {
  last7: MyCounts;
  last30: MyCounts;
}
