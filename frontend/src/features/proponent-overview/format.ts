import { formatDateTimeCuiaba } from '@/features/projects/format';
import type { AccessKey } from '@/features/projects/access-filter';

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** O prazo de uma complementação em texto: a data e quanto falta (ou que já venceu). Sempre texto, nunca só cor. */
export function dueLabel(dueAt: string, overdue: boolean | undefined, now: number) {
  const date = formatDateTimeCuiaba(dueAt);
  const ms = new Date(dueAt).getTime() - now;
  if (overdue || ms <= 0) return { text: `Prazo vencido em ${date}`, late: true };
  const days = Math.floor(ms / 86_400_000);
  const when = days >= 1 ? `vence em ${plural(days, 'dia', 'dias')}` : 'vence em menos de 24 horas';
  return { text: `Prazo até ${date} (${when})`, late: false };
}

/** Leva à lista de projetos já filtrada pela situação e pelo acesso escolhidos. */
export function projectsHref(situation: string | null, acesso: AccessKey) {
  const params = new URLSearchParams();
  if (situation) params.set('situacao', situation);
  if (acesso !== 'todos') params.set('acesso', acesso);
  const qs = params.toString();
  return qs ? `/dashboard/projetos?${qs}` : '/dashboard/projetos';
}

export const projectHref = (id: string) => `/dashboard/projetos/${id}`;
