import { Icons } from '@/components/icons';
import type { ComplementItem } from '../api/types';

/** Aviso, dentro do cartão do item, de que a equipe o devolveu: com a observação dela e se o proponente já mexeu nele. */
export function ReturnedNote({ item }: { item: ComplementItem }) {
  return (
    <div
      role='note'
      className='space-y-1 rounded-md border border-amber-500/50 bg-amber-500/10 p-3 text-sm'
    >
      <p className='flex items-center gap-1.5 font-medium'>
        <Icons.alertCircle className='size-4 shrink-0' aria-hidden />
        {item.answered ? 'Devolvido pela equipe · já alterado' : 'Devolvido pela equipe'}
      </p>
      {item.note && <p className='whitespace-pre-line'>{item.note}</p>}
    </div>
  );
}
