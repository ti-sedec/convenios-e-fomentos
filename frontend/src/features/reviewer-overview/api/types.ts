/** Contratos de GET /admin/projects/department/overview (backend/api/openapi.yaml). */

export interface ReviewerDepartment {
  id: string;
  name: string;
}

/** Dias vigentes de "parado" (parâmetro `stalled_after_days`). Zero = sem regra vigente. */
export interface ReviewerLimits {
  framing: number;
  awaiting_opinion: number;
}

export interface ReviewerCounts {
  /** Enquadramentos que esperam a decisão do departamento. */
  framing_pending: number;
  /** Enquadramentos confirmados, aguardando parecer. */
  awaiting_opinion: number;
  /** Arquivados nos últimos 30 dias. */
  archived_30d: number;
  /** Decisões do departamento nos últimos 30 dias (inclui as recusas, que tiram o projeto do alcance). */
  decided_30d: { confirmed: number; refused: number };
}

/** Um enquadramento que espera a decisão do departamento. */
export interface ReviewerItem {
  projectId: string;
  title: string;
  code?: string;
  institution?: string;
  /** Rodada pendente: quando e por quem o departamento foi indicado. */
  round: number;
  indicatedAt: string;
  indicatedBy?: string;
  /** Há quanto tempo o projeto está com o departamento (horas corridas). */
  sinceSeconds: number;
  /** Enviado fora do prazo eleitoral. */
  late?: boolean;
  /** Passou do limite de "parado" do enquadramento. */
  overLimit?: boolean;
}

export interface ReviewerOverview {
  /** Nulo só para quem passa por qualquer permissão e não tem departamento (Super Admin). */
  department: ReviewerDepartment | null;
  limits: ReviewerLimits;
  counts: ReviewerCounts;
  /** Os mais antigos primeiro; `items` traz até 15 e `total` conta todos. */
  pending: { total: number; items: ReviewerItem[] };
}

/** Contratos de GET /admin/projects/department/activity e /department/me. */

export type ReviewerActivityKind =
  | 'framing_indicated'
  | 'framing_confirmed'
  | 'framing_refused'
  | 'archived'
  | (string & {});

/** Quem agiu: a equipe vem com nome; o proponente e o sistema, só como tipo. `self` é a própria pessoa. */
export interface ReviewerActor {
  type: 'admin' | 'proponent' | 'system';
  name?: string;
  self?: boolean;
}

export interface ReviewerActivityItem {
  /** Texto: uma rodada gera dois marcos (a indicação e a decisão). */
  id: string;
  at: string;
  kind: ReviewerActivityKind;
  by: ReviewerActor;
  projectId: string;
  title: string;
  code?: string;
  /** Falso quando o Revisor não abre mais o projeto (depois da recusa ele sai do departamento). */
  linkable: boolean;
  data: { round?: number; reason?: string };
}

export interface ReviewerMyCounts {
  confirmed: number;
  refused: number;
}

/** O que a própria pessoa decidiu nos últimos 7 e 30 dias. */
export interface ReviewerMySummary {
  last7: ReviewerMyCounts;
  last30: ReviewerMyCounts;
}
