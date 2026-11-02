/** Ações do backend usadas para esconder elementos de UI (a autorização real é do backend). */
export const parliamentarianActions = {
  read: 'admin::catalog.parliamentarians.read',
  create: 'admin::catalog.parliamentarians.create',
  update: 'admin::catalog.parliamentarians.update',
  delete: 'admin::catalog.parliamentarians.delete'
} as const;

export type Chamber = 'state' | 'federal';

export const chamberLabels: Record<Chamber, string> = {
  state: 'Assembleia Legislativa (estadual)',
  federal: 'Câmara dos Deputados (federal)'
};

export interface Parliamentarian {
  id: string;
  name: string;
  chamber: Chamber;
  active: boolean;
  /** Citado em emendas de projetos: não pode ser excluído, só desativado. Ausente quando falso. */
  inUse?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface ParliamentarianInput {
  name: string;
  chamber: Chamber;
  active: boolean;
}
