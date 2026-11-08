import { Children, type ReactNode } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { cn } from '@/lib/utils';

/** Uma linha "rótulo: valor" das visualizações. Sem valor, a linha some (ou mostra "Não informado" com `showEmpty`). */
export function ViewRow({
  label,
  children,
  showEmpty = false
}: {
  label: string;
  children: ReactNode;
  showEmpty?: boolean;
}) {
  // Vários filhos chegam como lista (sempre "cheia"): vale o que sobra depois de tirar vazios e textos em branco.
  const filled = Children.toArray(children).some((c) => c !== '');
  if (!filled && !showEmpty) return null;
  return (
    <div className='grid gap-1 sm:grid-cols-[11rem_1fr]'>
      <dt className='text-muted-foreground text-sm'>{label}</dt>
      <dd className='text-sm break-words whitespace-pre-line'>
        {filled ? children : <span className='text-muted-foreground'>Não informado</span>}
      </dd>
    </div>
  );
}

/** Lista de linhas de uma seção. */
export function ViewList({ children, className }: { children: ReactNode; className?: string }) {
  return <dl className={cn('space-y-2', className)}>{children}</dl>;
}

/** Cartão de uma seção das visualizações; o `id` é a âncora usada pelos links "Ver dados". */
export function ViewCard({
  id,
  title,
  children
}: {
  id?: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <Card id={id} className='scroll-mt-20'>
      <CardHeader>
        <CardTitle className='text-base'>{title}</CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}
