import { maskPhone, maskZip } from '@/lib/masks';

export const onlyDigits = (s: string) => s.replace(/\D/g, '');

/** 12345678909 → 123.456.789-09 (entradas que não têm 11 dígitos voltam como estão). */
export function formatCpf(v: string) {
  const d = onlyDigits(v);
  return d.length === 11 ? `${d.slice(0, 3)}.${d.slice(3, 6)}.${d.slice(6, 9)}-${d.slice(9)}` : v;
}

export { formatCnpj } from '@/lib/cnpj';

/** 65999998888 → (65) 9 9999-8888; 6533334444 → (65) 3333-4444 (outras entradas voltam como estão). */
export function formatPhone(v: string) {
  const n = onlyDigits(v).length;
  return n === 10 || n === 11 ? maskPhone(v) : v;
}

/** 78000000 → 78000-000 (entradas que não têm 8 dígitos voltam como estão). */
export function formatZip(v: string) {
  return onlyDigits(v).length === 8 ? maskZip(v) : v;
}

/** AAAA-MM-DD → DD/MM/AAAA, sem passar por Date (que deslocaria o dia pelo fuso). */
export function formatDate(iso: string) {
  const [y, m, d] = iso.split('-');
  return y && m && d ? `${d}/${m}/${y}` : iso;
}

const brl = new Intl.NumberFormat('pt-BR', { style: 'currency', currency: 'BRL' });

/** Centavos → "R$ 1.234,56". */
export const formatBrl = (cents: number) => brl.format(cents / 100);

const cuiabaDateTime = new Intl.DateTimeFormat('pt-BR', {
  dateStyle: 'short',
  timeStyle: 'short',
  timeZone: 'America/Cuiaba'
});

/** Data e hora no fuso de Cuiabá (o mesmo dos prazos e dos e-mails), igual no servidor e no navegador. */
export function formatDateTimeCuiaba(value: string | Date | null | undefined) {
  if (!value) return '';
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? '' : cuiabaDateTime.format(d);
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/**
 * Segundos → "2 dias e 4 horas" (horas corridas). Mostra as duas maiores unidades que não são zero; menos de uma hora vira
 * minutos, e menos de um minuto, "menos de 1 minuto".
 */
export function formatDuration(totalSeconds: number) {
  const s = Math.max(0, Math.floor(totalSeconds));
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  if (days > 0) {
    return hours > 0
      ? `${plural(days, 'dia', 'dias')} e ${plural(hours, 'hora', 'horas')}`
      : plural(days, 'dia', 'dias');
  }
  if (hours > 0) {
    return minutes > 0
      ? `${plural(hours, 'hora', 'horas')} e ${plural(minutes, 'minuto', 'minutos')}`
      : plural(hours, 'hora', 'horas');
  }
  return minutes > 0 ? plural(minutes, 'minuto', 'minutos') : 'menos de 1 minuto';
}
