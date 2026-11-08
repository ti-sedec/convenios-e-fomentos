import type { Metadata } from 'next';
import { fullName } from '@/features/auth/full-name';
import { AnalystOverview } from '@/features/analyst-overview/components/analyst-overview';
import { projectActions } from '@/features/projects/api/types';
import { ProponentOverview } from '@/features/proponent-overview/components/proponent-overview';
import { ReviewerOverview } from '@/features/reviewer-overview/components/reviewer-overview';
import { getSession } from '@/lib/auth/session';
import { hasPermission, isProjectProponentSession } from '@/lib/auth/access';

export const metadata: Metadata = { title: 'Visão geral' };

export default async function OverviewPage() {
  const session = await getSession();
  const name = session ? fullName(session) : 'bem-vindo';
  // O Analista (quem pode assumir a análise) tem a visão geral da fila; o Super Admin, que é as duas coisas, cai nesta. Depois, o
  // Revisor (quem decide o enquadramento), que vem antes do Proponente: quem acumula os dois papéis cai na do trabalho da equipe. Por
  // fim, quem cadastra projetos (mesmo critério do menu "Meus projetos"). O restante da equipe segue com o texto, até ter a sua.
  if (await hasPermission(projectActions.startAnalysis)) return <AnalystOverview name={name} />;
  if (await hasPermission(projectActions.reviewFraming)) {
    return <ReviewerOverview name={name} hasDepartment={!!session?.departmentId} />;
  }
  if (await isProjectProponentSession()) return <ProponentOverview name={name} />;
  return (
    <p className='text-muted-foreground'>
      Olá, {name}. Os módulos de convênios e fomentos aparecerão aqui.
    </p>
  );
}
