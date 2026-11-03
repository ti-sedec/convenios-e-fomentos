'use client';

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import type { QueueScope } from '../api/types';

export const scopeKeys = ['equipe', 'meus'] as const;
export type ScopeKey = (typeof scopeKeys)[number];

/** Valor de `?escopo=` na URL → `scope` da API. */
export const scopeApi = (key: ScopeKey): QueueScope => (key === 'meus' ? 'mine' : 'team');

const options: { key: ScopeKey; label: string }[] = [
  { key: 'equipe', label: 'Toda a equipe' },
  { key: 'meus', label: 'Só os meus' }
];

/** "Só os meus" são os projetos cuja análise a pessoa assumiu. */
export function ScopeFilter({
  value,
  onChange
}: {
  value: ScopeKey;
  onChange: (key: ScopeKey) => void;
}) {
  return (
    <Tabs
      value={value}
      onValueChange={(v) => {
        const key = scopeKeys.find((k) => k === v);
        if (key) onChange(key);
      }}
    >
      <TabsList aria-label='Alcance da visão geral'>
        {options.map((o) => (
          <TabsTrigger key={o.key} value={o.key}>
            {o.label}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
