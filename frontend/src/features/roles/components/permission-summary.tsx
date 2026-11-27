'use client';

import { useQuery } from '@tanstack/react-query';
import { Badge } from '@/components/ui/badge';
import { Skeleton } from '@/components/ui/skeleton';
import { useCan } from '@/features/auth/session-provider';
import type { AdminPermission } from '@/lib/api/types';
import { catalogActionsQueryOptions, catalogConditionsQueryOptions } from '../api/queries';
import { roleActions } from '../api/types';
import { categoryLabel, sortCategories } from '../lib/labels';

/**
 * Permissões efetivas (somente leitura), agrupadas por categoria. Os nomes vêm do catálogo
 * (exige `roles.read`); sem ele, mostra o identificador da ação.
 */
export function PermissionSummary({
  permissions,
  isSuperAdmin
}: {
  permissions: AdminPermission[];
  isSuperAdmin: boolean;
}) {
  const can = useCan();
  const enabled = can(roleActions.read);
  const catalog = useQuery({ ...catalogActionsQueryOptions(), enabled });
  const conditions = useQuery({ ...catalogConditionsQueryOptions(), enabled });

  if (isSuperAdmin) {
    return <p className='text-muted-foreground text-sm'>Acesso total: o Super Admin pode tudo.</p>;
  }
  if (permissions.length === 0) {
    return <p className='text-muted-foreground text-sm'>Sem nenhuma permissão.</p>;
  }
  if (enabled && catalog.isLoading) return <Skeleton className='h-24 w-full' />;

  const byId = new Map(
    (catalog.data ?? []).flatMap((g) => g.actions.map((a) => [a.id, { ...a }] as const))
  );
  const conditionName = (id: string) => conditions.data?.find((c) => c.id === id)?.name ?? id;

  const groups = new Map<string, AdminPermission[]>();
  for (const p of permissions) {
    const category = byId.get(p.action)?.category ?? 'other';
    groups.set(category, [...(groups.get(category) ?? []), p]);
  }
  const ordered = sortCategories([...groups].map(([category, items]) => ({ category, items })));

  return (
    <div className='grid gap-4'>
      {ordered.map((g) => (
        <section key={g.category} className='grid gap-1.5'>
          <h4 className='text-sm font-medium'>
            {g.category === 'other' ? 'Outras' : categoryLabel(g.category)}
          </h4>
          <ul className='grid gap-1.5'>
            {g.items.map((p) => (
              <li
                key={`${p.action}:${p.subject}`}
                className='flex flex-wrap items-center gap-1.5 text-sm'
              >
                <span>{byId.get(p.action)?.name ?? p.action}</span>
                {p.fields && (
                  <Badge variant='outline'>
                    {p.fields.length === 0 ? 'nenhum campo' : `campos: ${p.fields.join(', ')}`}
                  </Badge>
                )}
                {p.conditions.map((c) => (
                  <Badge key={c} variant='secondary'>
                    {conditionName(c)}
                  </Badge>
                ))}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
