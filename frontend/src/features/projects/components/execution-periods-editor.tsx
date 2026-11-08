'use client';

import { Icons } from '@/components/icons';
import { Button } from '@/components/ui/button';
import { DateInput } from '@/components/ui/date-input';
import { Input } from '@/components/ui/input';
import { useFormReadOnly } from '@/lib/form-context';
import type { ExecutionPeriod } from '../api/types';
import { MunicipalitySelect } from './municipality-select';

const MAX = 50;
const UF = 'MT';

/** Linha no formulário: `key` é só da tela (identifica a linha enquanto ela é editada, inclusive ao remover) e não vai ao servidor. */
export type ExecutionPeriodRow = Pick<
  ExecutionPeriod,
  'startDate' | 'endDate' | 'municipalityIbge' | 'placeDetail'
> & { key: string };

let nextKey = 0;
const newKey = () => `period-${nextKey++}`;

export const emptyPeriod = (): ExecutionPeriodRow => ({
  key: newKey(),
  startDate: '',
  endDate: '',
  municipalityIbge: '',
  placeDetail: ''
});

export const toRow = (e: ExecutionPeriod): ExecutionPeriodRow => ({
  key: newKey(),
  startDate: e.startDate,
  endDate: e.endDate,
  municipalityIbge: e.municipalityIbge,
  placeDetail: e.placeDetail
});

export const fromRow = ({ key: _key, ...e }: ExecutionPeriodRow) => e;

/** Linhas iniciais do formulário: sempre pelo menos uma, para o proponente ver onde preencher. */
export const rowsOf = (periods: ExecutionPeriod[]): ExecutionPeriodRow[] =>
  periods.length > 0 ? periods.map(toRow) : [emptyPeriod()];

/**
 * Períodos e locais de execução do projeto: cada linha tem início e término previstos, o município de execução (só Mato Grosso) e o
 * detalhe do local (opcional). Períodos que se sobrepõem são permitidos, como dois locais no mesmo dia.
 */
export function ExecutionPeriodsEditor({
  value,
  onChange
}: {
  value: ExecutionPeriodRow[];
  onChange: (next: ExecutionPeriodRow[]) => void;
}) {
  const readOnly = useFormReadOnly();
  const set = (key: string, patch: Partial<ExecutionPeriodRow>) =>
    onChange(value.map((r) => (r.key === key ? { ...r, ...patch } : r)));

  return (
    <fieldset className='space-y-3'>
      <legend className='text-sm leading-snug font-medium'>Períodos e locais de execução *</legend>
      <p className='text-muted-foreground text-sm'>
        Informe o início e o término previstos de cada período, com o município (apenas Mato Grosso)
        e o detalhe do local. Se o projeto acontece em mais de um período ou lugar, adicione uma
        linha para cada um.
      </p>
      {value.map((r, i) => {
        const n = i + 1;
        const invalidEnd = r.startDate !== '' && r.endDate !== '' && r.endDate < r.startDate;
        return (
          <div key={r.key} className='space-y-3 rounded-lg border p-3'>
            <div className='flex items-center justify-between gap-2'>
              <p className='text-sm font-medium'>Período {n}</p>
              {!readOnly && value.length > 1 && (
                <Button
                  type='button'
                  size='icon'
                  variant='ghost'
                  aria-label={`Remover período ${n}`}
                  onClick={() => onChange(value.filter((x) => x.key !== r.key))}
                >
                  <Icons.trash aria-hidden />
                </Button>
              )}
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <label className='text-xs font-medium' htmlFor={`${r.key}-start`}>
                  Início previsto *
                </label>
                <DateInput
                  id={`${r.key}-start`}
                  value={r.startDate}
                  onChange={(startDate) => set(r.key, { startDate })}
                />
              </div>
              <div className='space-y-1.5'>
                <label className='text-xs font-medium' htmlFor={`${r.key}-end`}>
                  Término previsto *
                </label>
                <DateInput
                  id={`${r.key}-end`}
                  min={r.startDate || undefined}
                  value={r.endDate}
                  aria-invalid={invalidEnd || undefined}
                  aria-describedby={invalidEnd ? `${r.key}-end-error` : undefined}
                  onChange={(endDate) => set(r.key, { endDate })}
                />
                {invalidEnd && (
                  <p id={`${r.key}-end-error`} role='alert' className='text-destructive text-xs'>
                    O término não pode ser anterior ao início.
                  </p>
                )}
              </div>
              <div className='space-y-1.5'>
                <label className='text-xs font-medium' htmlFor={`${r.key}-municipality`}>
                  Município de execução *
                </label>
                <MunicipalitySelect
                  id={`${r.key}-municipality`}
                  uf={UF}
                  value={r.municipalityIbge}
                  onChange={(municipalityIbge) => set(r.key, { municipalityIbge })}
                />
              </div>
              <div className='space-y-1.5'>
                <label className='text-xs font-medium' htmlFor={`${r.key}-detail`}>
                  Detalhe do local
                </label>
                <Input
                  id={`${r.key}-detail`}
                  maxLength={500}
                  placeholder='Endereço, trecho ou coordenadas, se ajudar'
                  value={r.placeDetail}
                  onChange={(e) => set(r.key, { placeDetail: e.target.value })}
                />
              </div>
            </div>
          </div>
        );
      })}
      {!readOnly && value.length < MAX && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => onChange([...value, emptyPeriod()])}
        >
          <Icons.add aria-hidden /> Adicionar período e local
        </Button>
      )}
    </fieldset>
  );
}
