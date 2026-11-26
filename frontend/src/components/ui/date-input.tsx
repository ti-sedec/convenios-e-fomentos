'use client';

import * as React from 'react';

import { Button } from '@/components/ui/button';
import { Calendar } from '@/components/ui/calendar';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Icons } from '@/components/icons';
import { cn } from '@/lib/utils';

/** AAAA-MM-DD → DD/MM/AAAA (vazio se não for uma data válida). */
function isoToText(iso: string | undefined) {
  const [y, m, d] = (iso ?? '').split('-');
  return y && m && d ? `${d}/${m}/${y}` : '';
}

/** DD/MM/AAAA → AAAA-MM-DD, ou '' se incompleta ou inexistente (ex.: 31/02/2026). */
function textToIso(text: string) {
  const m = /^(\d{2})\/(\d{2})\/(\d{4})$/.exec(text);
  if (!m) return '';
  const [, d, mo, y] = m;
  const date = new Date(Number(y), Number(mo) - 1, Number(d));
  const valid =
    date.getFullYear() === Number(y) &&
    date.getMonth() === Number(mo) - 1 &&
    date.getDate() === Number(d);
  return valid ? `${y}-${mo}-${d}` : '';
}

/** Aplica a máscara DD/MM/AAAA enquanto a pessoa digita. */
function maskDate(raw: string) {
  const digits = raw.replace(/\D/g, '').slice(0, 8);
  if (digits.length <= 2) return digits;
  if (digits.length <= 4) return `${digits.slice(0, 2)}/${digits.slice(2)}`;
  return `${digits.slice(0, 2)}/${digits.slice(2, 4)}/${digits.slice(4)}`;
}

function isoToDate(iso: string | undefined) {
  const [y, m, d] = (iso ?? '').split('-').map(Number);
  return y && m && d ? new Date(y, m - 1, d) : undefined;
}

function dateToIso(date: Date) {
  const p = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${p(date.getMonth() + 1)}-${p(date.getDate())}`;
}

type DateInputProps = Omit<
  React.ComponentProps<typeof Input>,
  'value' | 'onChange' | 'type' | 'min' | 'max'
> & {
  /** Valor no formato AAAA-MM-DD (ou vazio). */
  value: string;
  onChange: (iso: string) => void;
  /** Limites no formato AAAA-MM-DD. */
  min?: string | number;
  max?: string | number;
};

/**
 * Campo de data no padrão brasileiro (DD/MM/AAAA) com calendário.
 * Substitui `<input type='date'>`, cujo formato depende do idioma do navegador.
 * O valor continua em AAAA-MM-DD, igual ao input nativo.
 */
export function DateInput({
  value,
  onChange,
  min,
  max,
  className,
  disabled,
  ...inputProps
}: DateInputProps) {
  const [text, setText] = React.useState(isoToText(value));
  const [open, setOpen] = React.useState(false);

  // Sincroniza quando o valor muda por fora (reset do formulário, calendário, filtro limpo). É o ajuste de estado durante a renderização
  // que a documentação do React indica no lugar de um efeito: não faz uma renderização a mais com o texto antigo na tela.
  const [syncedValue, setSyncedValue] = React.useState(value);
  if (value !== syncedValue) {
    setSyncedValue(value);
    setText((current) => (textToIso(current) === (value ?? '') ? current : isoToText(value)));
  }

  const minDate = isoToDate(min == null ? undefined : String(min));
  const maxDate = isoToDate(max == null ? undefined : String(max));

  // Sem isto o seletor de ano do calendário termina no ano corrente (padrão do react-day-picker),
  // o que impede escolher datas futuras como a validade de uma certidão ou o fim de uma vigência.
  const [thisYear] = React.useState(() => new Date().getFullYear());
  const startMonth = minDate ?? new Date(thisYear - 100, 0);
  const endMonth = maxDate ?? new Date(thisYear + 20, 11);

  return (
    <div className={cn('relative', className)}>
      <Input
        {...inputProps}
        value={text}
        inputMode='numeric'
        autoComplete='off'
        placeholder='dd/mm/aaaa'
        maxLength={10}
        disabled={disabled}
        className='pe-9'
        onChange={(e) => {
          const next = maskDate(e.target.value);
          setText(next);
          onChange(textToIso(next));
        }}
      />
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type='button'
              variant='ghost'
              size='icon'
              disabled={disabled}
              aria-label='Abrir calendário'
              className='absolute inset-e-0 top-0 size-8'
            />
          }
        >
          <Icons.calendar className='text-muted-foreground size-4' />
        </PopoverTrigger>
        <PopoverContent className='w-auto p-0' align='end'>
          <Calendar
            mode='single'
            autoFocus
            captionLayout='dropdown'
            startMonth={startMonth}
            endMonth={endMonth}
            selected={isoToDate(value)}
            defaultMonth={isoToDate(value)}
            disabled={(date) => (!!minDate && date < minDate) || (!!maxDate && date > maxDate)}
            onSelect={(date) => {
              if (!date) return;
              const iso = dateToIso(date);
              setText(isoToText(iso));
              onChange(iso);
              setOpen(false);
            }}
          />
        </PopoverContent>
      </Popover>
    </div>
  );
}
