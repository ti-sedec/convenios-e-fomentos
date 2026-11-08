import { toast } from 'sonner';
import { ApiError } from '@/lib/api/errors';
import { errorMessage } from '@/lib/errors';

/** Erros que querem dizer "o estado do projeto mudou desde que a tela abriu": a tela recarrega o projeto. */
const staleCodes = new Set([
  'framing_already_decided',
  'framing_round_stale',
  'framing_pending',
  'not_responsible_analyst',
  'analysis_not_started',
  'project_stage_invalid',
  'project_not_submitted',
  'project_archived'
]);

/** Mensagem em português para os erros do fluxo de análise e enquadramento. */
export function staffErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const d = err.details ?? {};
    switch (err.code) {
      case 'file_not_opened': {
        const files = Array.isArray(d.files) ? (d.files as { filename: string }[]) : [];
        const names = files.map((f) => f.filename).join(', ');
        return `Visualize todos os arquivos deste documento antes de decidir.${names ? ` Faltam: ${names}.` : ''}`;
      }
      case 'file_not_read': {
        const files = Array.isArray(d.files) ? (d.files as { filename: string }[]) : [];
        const names = files.map((f) => f.filename).join(', ');
        return `Role até o fim de cada arquivo antes de aprovar.${names ? ` Faltam: ${names}.` : ''}`;
      }
      case 'review_incomplete': {
        const pending = Number(d.pending ?? 0);
        const rejected = Number(d.rejected ?? 0);
        const blockers = Number(d.blockers ?? 0);
        return `Ainda há ${pending} item(ns) pendente(s), ${rejected} reprovado(s) e ${blockers} impedimento(s) na conferência.`;
      }
      case 'framing_already_decided':
      case 'framing_round_stale':
        return 'Outra pessoa já decidiu, ou a indicação mudou. A página foi atualizada.';
      case 'analysis_not_started':
        return 'Inicie a análise antes de conferir.';
      case 'forbidden':
        return 'Você não tem permissão para esta ação.';
    }
  }
  return errorMessage(err);
}

/** Mostra o erro e informa se a tela deve recarregar o projeto (alguém mudou o estado, ou a permissão não vale mais). */
export function reportStaffError(err: unknown): { reload: boolean } {
  toast.error(staffErrorMessage(err));
  const reload = err instanceof ApiError && (staleCodes.has(err.code) || err.status === 403);
  return { reload };
}
