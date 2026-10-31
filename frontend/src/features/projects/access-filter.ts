import type { AccessFilter } from '@/features/proponent-overview/api/types';

/** Valores de `?acesso=` na URL: tudo, só os projetos do proponente ou só os compartilhados com ele. */
export const accessKeys = ['todos', 'meus', 'compartilhados'] as const;
export type AccessKey = (typeof accessKeys)[number];

export const accessOptions: {
  key: AccessKey;
  label: string;
  /** Valor de `access` na API; `todos` não manda nada (o servidor já limita ao que é da pessoa). */
  api: AccessFilter | undefined;
  count: (by: { owner: number; member: number }) => number;
}[] = [
  { key: 'todos', label: 'Todos', api: undefined, count: (b) => b.owner + b.member },
  { key: 'meus', label: 'Meus', api: 'owner', count: (b) => b.owner },
  { key: 'compartilhados', label: 'Compartilhados comigo', api: 'member', count: (b) => b.member }
];

export const accessApi = (key: AccessKey) => accessOptions.find((o) => o.key === key)?.api;
