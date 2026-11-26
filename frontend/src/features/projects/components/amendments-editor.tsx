'use client';

import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Icons } from '@/components/icons';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select';
import { parliamentariansQueryOptions } from '@/features/parliamentarians/api/queries';
import type { Parliamentarian } from '@/features/parliamentarians/api/types';
import { useFormReadOnly } from '@/lib/form-context';
import type { Amendment } from '../api/types';
import { MoneyInput } from './money-input';

const OTHER = '__other';
const MAX = 20;

/** Linha de emenda no formulário: `other` é só da tela (parlamentar fora da lista) e não vai ao servidor. */
export type AmendmentRow = Amendment & { other?: boolean };

export const toRow = (a: Amendment): AmendmentRow => ({
  ...a,
  other: a.parliamentarianId === '' && a.parliamentarianName !== ''
});

export const fromRow = ({ other: _other, ...a }: AmendmentRow): Amendment => a;

/**
 * Seletor do parlamentar. Não é o `<select>` nativo: a lista nativa do navegador fecha sozinha com qualquer scroll, perda de foco ou
 * mudança no DOM (o clique rápido abria e fechava a lista na hora). O `Select` do shadcn desenha a própria lista.
 */
function ParliamentarianSelect({
  id,
  value,
  options,
  disabled,
  onPick
}: {
  id: string;
  value: string;
  options: Parliamentarian[];
  disabled: boolean;
  onPick: (value: string) => void;
}) {
  const items = [
    ...options.map((p) => ({
      value: p.id,
      label: `${p.name}${p.chamber === 'federal' ? ' (federal)' : ''}`
    })),
    { value: OTHER, label: 'Outro (informar o nome)' }
  ];
  return (
    <Select items={items} value={value} disabled={disabled} onValueChange={(v) => onPick(v ?? '')}>
      <SelectTrigger id={id} className='w-full'>
        <SelectValue placeholder='Escolha…' />
      </SelectTrigger>
      <SelectContent>
        <SelectGroup>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}

/** Emendas parlamentares do projeto: parlamentar (do cadastro ou por extenso), número, ano e valor. */
export function AmendmentsEditor({
  value,
  onChange
}: {
  value: AmendmentRow[];
  onChange: (next: AmendmentRow[]) => void;
}) {
  const readOnly = useFormReadOnly();
  const list = useQuery(parliamentariansQueryOptions());
  const usedIds = new Set(value.map((a) => a.parliamentarianId));
  // Parlamentar desativado só aparece onde já foi escolhido.
  const options = (list.data ?? []).filter((p) => p.active || usedIds.has(p.id));
  const set = (i: number, patch: Partial<AmendmentRow>) =>
    onChange(value.map((a, idx) => (idx === i ? { ...a, ...patch } : a)));
  const [year] = useState(() => new Date().getFullYear());

  return (
    <fieldset className='space-y-3'>
      <legend className='text-sm leading-snug font-medium'>Emendas parlamentares *</legend>
      <p className='text-muted-foreground text-sm'>
        Informe cada emenda com o deputado, o número, o ano e o valor. Os ofícios do deputado e da
        Casa Civil são anexados na etapa de documentos.
      </p>
      {value.map((a, i) => (
        <div
          key={i}
          className='grid gap-2 rounded-lg border p-3 sm:grid-cols-[1fr_8rem_6rem_11rem_auto] sm:items-end'
        >
          <div className='space-y-1.5'>
            <label className='text-xs font-medium' htmlFor={`amendment-${i}-who`}>
              Parlamentar
            </label>
            <ParliamentarianSelect
              id={`amendment-${i}-who`}
              value={a.other ? OTHER : a.parliamentarianId}
              options={options}
              disabled={readOnly}
              onPick={(v) => {
                if (v === OTHER) set(i, { other: true, parliamentarianId: '' });
                else set(i, { other: false, parliamentarianId: v, parliamentarianName: '' });
              }}
            />
            {a.other && (
              <Input
                aria-label={`Nome do parlamentar da emenda ${i + 1}`}
                maxLength={120}
                placeholder='Nome completo do parlamentar'
                value={a.parliamentarianName}
                onChange={(e) => set(i, { parliamentarianName: e.target.value })}
              />
            )}
          </div>
          <div className='space-y-1.5'>
            <label className='text-xs font-medium' htmlFor={`amendment-${i}-number`}>
              Nº da emenda
            </label>
            <Input
              id={`amendment-${i}-number`}
              maxLength={30}
              value={a.number}
              onChange={(e) => set(i, { number: e.target.value })}
            />
          </div>
          <div className='space-y-1.5'>
            <label className='text-xs font-medium' htmlFor={`amendment-${i}-year`}>
              Ano
            </label>
            <Input
              id={`amendment-${i}-year`}
              inputMode='numeric'
              placeholder={String(year)}
              value={a.year ? String(a.year) : ''}
              onChange={(e) =>
                set(i, { year: Number(e.target.value.replace(/\D/g, '').slice(0, 4)) })
              }
            />
          </div>
          <div className='space-y-1.5'>
            <label className='text-xs font-medium' htmlFor={`amendment-${i}-amount`}>
              Valor
            </label>
            <MoneyInput
              id={`amendment-${i}-amount`}
              value={a.amountCents}
              onChange={(amountCents) => set(i, { amountCents })}
            />
          </div>
          {!readOnly && (
            <Button
              type='button'
              size='icon'
              variant='ghost'
              aria-label={`Remover emenda ${i + 1}`}
              onClick={() => onChange(value.filter((_, idx) => idx !== i))}
            >
              <Icons.trash aria-hidden />
            </Button>
          )}
        </div>
      ))}
      {!readOnly && value.length < MAX && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() =>
            onChange([
              ...value,
              {
                id: '',
                parliamentarianId: '',
                parliamentarianName: '',
                number: '',
                year,
                amountCents: 0
              }
            ])
          }
        >
          <Icons.add aria-hidden /> Adicionar emenda
        </Button>
      )}
    </fieldset>
  );
}
