'use client';

import { Badge } from '@/components/ui/badge';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { accessKeys, accessOptions, type AccessKey } from '../access-filter';

/** Seletor "Todos / Meus / Compartilhados comigo". `totals` mostra quantos projetos há em cada opção, sem depender da escolhida. */
export function AccessFilter({
  value,
  onChange,
  totals
}: {
  value: AccessKey;
  onChange: (key: AccessKey) => void;
  totals?: { owner: number; member: number };
}) {
  return (
    <Tabs
      value={value}
      onValueChange={(v) => {
        const key = accessKeys.find((k) => k === v);
        if (key) onChange(key);
      }}
    >
      <TabsList aria-label='Projetos por acesso' className='h-auto flex-wrap justify-start'>
        {accessOptions.map((o) => (
          <TabsTrigger key={o.key} value={o.key}>
            {o.label}
            {totals && (
              <Badge variant='secondary' className='ml-1 tabular-nums'>
                {o.count(totals)}
              </Badge>
            )}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
