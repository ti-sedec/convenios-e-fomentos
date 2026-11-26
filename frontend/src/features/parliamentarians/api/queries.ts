import { queryOptions } from '@tanstack/react-query';
import { listParliamentarians } from './service';

export const parliamentarianKeys = {
  all: ['parliamentarians'] as const,
  list: () => [...parliamentarianKeys.all, 'list'] as const
};

export const parliamentariansQueryOptions = () =>
  queryOptions({
    queryKey: parliamentarianKeys.list(),
    queryFn: listParliamentarians,
    // Cadastro que quase não muda: reler a cada foco da janela re-renderizava os selects abertos e os fechava.
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false
  });
