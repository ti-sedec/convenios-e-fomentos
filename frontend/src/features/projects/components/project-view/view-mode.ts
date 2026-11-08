import { projectActions } from '../../api/types';

/**
 * Como a equipe vê o projeto: o Revisor do Departamento (decide o enquadramento, não confere item a item) abre no resumo enxuto; os
 * demais perfis abrem na visualização completa. Quem tem as duas permissões (Super Admin) segue como o Analista. É só apresentação:
 * a autorização é do servidor.
 */
export type StaffViewMode = 'summary' | 'full';

export function staffViewModeOf(can: (action: string) => boolean): StaffViewMode {
  return can(projectActions.reviewFraming) && !can(projectActions.reviewItem) ? 'summary' : 'full';
}
