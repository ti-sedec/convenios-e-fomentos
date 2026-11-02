'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { AlertModal } from '@/components/modal/alert-modal';
import { QueryError } from '@/components/query-error';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table';
import { useCan } from '@/features/auth/session-provider';
import { errorMessage } from '@/lib/errors';
import { parliamentarianKeys, parliamentariansQueryOptions } from '../api/queries';
import { deleteParliamentarian } from '../api/service';
import { chamberLabels, parliamentarianActions, type Parliamentarian } from '../api/types';
import { ParliamentarianDialog } from './parliamentarian-dialog';

export function ParliamentariansList() {
  const can = useCan();
  const qc = useQueryClient();
  const list = useQuery(parliamentariansQueryOptions());
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<Parliamentarian | null>(null);
  const [toDelete, setToDelete] = useState<Parliamentarian | null>(null);

  const remove = useMutation({
    mutationFn: deleteParliamentarian,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: parliamentarianKeys.all });
      toast.success('Parlamentar excluído.');
      setToDelete(null);
    },
    onError: (err) => {
      toast.error(errorMessage(err));
      setToDelete(null);
    }
  });

  if (list.isError) return <QueryError error={list.error} what='os parlamentares' />;
  if (list.isLoading) return <Skeleton className='h-40 w-full' />;

  const term = search.trim().toLocaleLowerCase('pt-BR');
  const rows = (list.data ?? []).filter(
    (p) => !term || p.name.toLocaleLowerCase('pt-BR').includes(term)
  );

  return (
    <>
      <Input
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        placeholder='Buscar por nome'
        aria-label='Buscar parlamentar por nome'
        className='mb-4 max-w-sm'
      />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Nome</TableHead>
            <TableHead>Casa legislativa</TableHead>
            <TableHead>Situação</TableHead>
            <TableHead className='w-px' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={4} className='text-muted-foreground text-center'>
                {list.data?.length
                  ? 'Nenhum parlamentar encontrado.'
                  : 'Nenhum parlamentar cadastrado.'}
              </TableCell>
            </TableRow>
          )}
          {rows.map((p) => (
            <TableRow key={p.id} className={p.active ? undefined : 'text-muted-foreground'}>
              <TableCell className='font-medium'>{p.name}</TableCell>
              <TableCell>{chamberLabels[p.chamber]}</TableCell>
              <TableCell>
                <div className='flex flex-wrap gap-1'>
                  <Badge variant={p.active ? 'secondary' : 'outline'}>
                    {p.active ? 'Ativo' : 'Inativo'}
                  </Badge>
                  {p.inUse && (
                    <Badge
                      variant='outline'
                      title='Citado em projetos: não pode ser excluído, apenas desativado.'
                    >
                      Em uso
                    </Badge>
                  )}
                </div>
              </TableCell>
              <TableCell>
                <div className='flex justify-end gap-1'>
                  {can(parliamentarianActions.update) && (
                    <Button size='sm' variant='ghost' onClick={() => setEditing(p)}>
                      <Icons.edit aria-hidden /> Editar
                    </Button>
                  )}
                  {can(parliamentarianActions.delete) && !p.inUse && (
                    <Button size='sm' variant='ghost' onClick={() => setToDelete(p)}>
                      <Icons.trash aria-hidden /> Excluir
                    </Button>
                  )}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {editing && (
        <ParliamentarianDialog
          key={editing.id}
          parliamentarian={editing}
          open
          onOpenChange={(open) => !open && setEditing(null)}
        />
      )}
      <AlertModal
        isOpen={toDelete !== null}
        onClose={() => setToDelete(null)}
        onConfirm={() => toDelete && remove.mutate(toDelete.id)}
        loading={remove.isPending}
        title={`Excluir "${toDelete?.name ?? ''}"?`}
        description='Se houver projetos com emendas deste parlamentar, a exclusão é recusada; desative-o em vez de excluir.'
        confirmLabel='Excluir'
        cancelLabel='Cancelar'
      />
    </>
  );
}
