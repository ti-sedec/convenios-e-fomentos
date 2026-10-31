import type { Metadata } from 'next';
import { fullName } from '@/features/auth/full-name';
import { ProponentOverview } from '@/features/proponent-overview/components/proponent-overview';
import { getSession } from '@/lib/auth/session';
import { isProjectProponentSession } from '@/lib/auth/access';

export const metadata: Metadata = { title: 'Visão geral' };

export default async function OverviewPage() {
  const session = await getSession();
  const name = session ? fullName(session) : 'bem-vindo';
  // Mesmo critério do menu "Meus projetos": quem cadastra projetos. A equipe segue com o texto, até ter a sua visão geral.
  if (await isProjectProponentSession()) return <ProponentOverview name={name} />;
  return (
    <p className='text-muted-foreground'>
      Olá, {name}. Os módulos de convênios e fomentos aparecerão aqui.
    </p>
  );
}
