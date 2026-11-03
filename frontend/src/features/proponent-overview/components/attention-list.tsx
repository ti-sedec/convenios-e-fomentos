'use client';

import Link from 'next/link';
import { useState } from 'react';
import { Icons } from '@/components/icons';
import { buttonVariants } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { formatDateTimeCuiaba } from '@/features/projects/format';
import { dueLabel, projectHref } from '../format';
import type { AttentionItem } from '../api/types';

const levelLabels = { read: 'leitura', edit: 'edição' } as const;
const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

interface Row {
  key: string;
  icon: keyof typeof Icons;
  title: string;
  detail: string;
  late?: boolean;
  href: string;
  cta: string;
}

function rowOf(it: AttentionItem, now: number): Row {
  const title = it.title || 'Projeto sem título';
  const href = projectHref(it.projectId);
  switch (it.kind) {
    case 'complement_open': {
      const due = it.dueAt ? dueLabel(it.dueAt, it.overdue, now) : null;
      return {
        key: `${it.kind}:${it.projectId}`,
        icon: due?.late ? 'alertCircle' : 'warning',
        title: `${title}: a equipe pediu correções`,
        detail: [it.items ? plural(it.items, 'item devolvido', 'itens devolvidos') : '', due?.text]
          .filter(Boolean)
          .join(' · '),
        late: due?.late,
        href,
        cta: due?.late ? 'Abrir' : 'Responder'
      };
    }
    case 'signature_pending':
      return {
        key: `${it.kind}:${it.projectId}`,
        icon: 'edit',
        title: `${title}: aguardando assinatura`,
        detail: it.declaration ? `${it.declaration}, no MT-ID` : 'Declaração no MT-ID',
        href,
        cta: 'Abrir'
      };
    case 'draft':
      return {
        key: `${it.kind}:${it.projectId}`,
        icon: 'edit',
        title: `${title}: rascunho em andamento`,
        detail: [
          it.blocking === undefined
            ? ''
            : it.blocking > 0
              ? `${plural(it.blocking, 'pendência impede', 'pendências impedem')} o envio`
              : 'Sem pendências: pronto para enviar',
          it.updatedAt ? `alterado em ${formatDateTimeCuiaba(it.updatedAt)}` : ''
        ]
          .filter(Boolean)
          .join(' · '),
        href,
        cta: 'Continuar'
      };
    case 'shared_new':
      return {
        key: `${it.kind}:${it.projectId}`,
        icon: 'share',
        title: `${title}: compartilhado com você`,
        detail: [
          it.invitedBy ? `Por ${it.invitedBy}` : '',
          it.level ? `acesso de ${levelLabels[it.level]}` : ''
        ]
          .filter(Boolean)
          .join(' · '),
        href,
        cta: 'Abrir'
      };
  }
}

/** "Precisa da sua atenção": o que o proponente pode resolver agora. Some quando não há nada. */
export function AttentionList({
  items,
  profileIncomplete
}: {
  items: AttentionItem[];
  profileIncomplete: boolean;
}) {
  // Lido uma vez ao montar: o texto do prazo não depende de um relógio que mudaria entre renderizações.
  const [now] = useState(() => Date.now());
  const rows: Row[] = [
    ...(profileIncomplete
      ? [
          {
            key: 'profile',
            icon: 'profile' as const,
            title: 'Complete o seu perfil de proponente',
            detail: 'Celular, e-mail, município e cargo já vêm preenchidos nos novos projetos.',
            href: '/dashboard/perfil-proponente',
            cta: 'Completar'
          }
        ]
      : []),
    ...items.map((it) => rowOf(it, now))
  ];
  if (rows.length === 0) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Precisa da sua atenção</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className='divide-y'>
          {rows.map((r) => {
            const Icon = Icons[r.icon];
            return (
              <li key={r.key} className='flex flex-wrap items-center justify-between gap-2 py-2'>
                <div className='flex min-w-0 items-start gap-2'>
                  <Icon aria-hidden className='mt-0.5 size-4 shrink-0' />
                  <div className='min-w-0'>
                    <p className='text-sm font-medium'>{r.title}</p>
                    {r.detail && (
                      <p
                        className={`text-xs ${r.late ? 'text-destructive font-medium' : 'text-muted-foreground'}`}
                      >
                        {r.detail}
                      </p>
                    )}
                  </div>
                </div>
                <Link href={r.href} className={buttonVariants({ size: 'sm', variant: 'outline' })}>
                  {r.cta}
                </Link>
              </li>
            );
          })}
        </ul>
      </CardContent>
    </Card>
  );
}
