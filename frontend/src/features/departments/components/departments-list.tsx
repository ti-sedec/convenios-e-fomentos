'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { AlertModal } from '@/components/modal/alert-modal';
import { QueryError } from '@/components/query-error';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
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
import { departmentKeys, departmentsQueryOptions } from '../api/queries';
import { deleteDepartment } from '../api/service';
import { departmentActions, type Department } from '../api/types';
import { DepartmentDialog } from './department-dialog';

export function DepartmentsList() {
  const can = useCan();
  const qc = useQueryClient();
  const departments = useQuery(departmentsQueryOptions());
  const [editing, setEditing] = useState<Department | null>(null);
  const [toDelete, setToDelete] = useState<Department | null>(null);

  const remove = useMutation({
    mutationFn: deleteDepartment,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: departmentKeys.all });
      toast.success('Departamento excluído.');
      setToDelete(null);
    },
    onError: (err) => {
      toast.error(errorMessage(err));
      setToDelete(null);
    }
  });

  if (departments.isError) return <QueryError error={departments.error} what='os departamentos' />;
  if (departments.isLoading) return <Skeleton className='h-40 w-full' />;

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Departamento</TableHead>
            <TableHead>Área finalística</TableHead>
            <TableHead>Administradores</TableHead>
            <TableHead>Revisores ativos</TableHead>
            <TableHead>Projetos</TableHead>
            <TableHead className='w-px' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {departments.data?.length === 0 && (
            <TableRow>
              <TableCell colSpan={6} className='text-muted-foreground text-center'>
                Nenhum departamento cadastrado.
              </TableCell>
            </TableRow>
          )}
          {departments.data?.map((d) => (
            <TableRow key={d.id}>
              <TableCell className='font-medium'>{d.name}</TableCell>
              <TableCell>
                {d.isFinalistic ? (
                  <Badge>Finalística</Badge>
                ) : (
                  <span className='text-muted-foreground'>Não</span>
                )}
              </TableCell>
              <TableCell className='tabular-nums'>{d.userCount}</TableCell>
              <TableCell className='tabular-nums'>
                {d.reviewerCount}
                {d.isFinalistic && d.reviewerCount === 0 && (
                  <Badge variant='destructive' className='ml-2'>
                    Sem revisor
                  </Badge>
                )}
              </TableCell>
              <TableCell className='tabular-nums'>{d.projectCount}</TableCell>
              <TableCell>
                <div className='flex justify-end gap-1'>
                  {can(departmentActions.update) && (
                    <Button size='sm' variant='ghost' onClick={() => setEditing(d)}>
                      <Icons.edit aria-hidden /> Editar
                    </Button>
                  )}
                  {can(departmentActions.delete) && (
                    <Button
                      size='sm'
                      variant='ghost'
                      disabled={d.userCount + d.projectCount > 0}
                      title={
                        d.userCount + d.projectCount > 0
                          ? 'Há administradores ou projetos vinculados.'
                          : undefined
                      }
                      onClick={() => setToDelete(d)}
                    >
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
        <DepartmentDialog
          key={editing.id}
          department={editing}
          open
          onOpenChange={(open) => !open && setEditing(null)}
        />
      )}
      <AlertModal
        isOpen={toDelete !== null}
        onClose={() => setToDelete(null)}
        onConfirm={() => toDelete && remove.mutate(toDelete.id)}
        loading={remove.isPending}
        title={`Excluir o departamento "${toDelete?.name ?? ''}"?`}
        description='A exclusão não pode ser desfeita.'
        confirmLabel='Excluir'
        cancelLabel='Cancelar'
      />
    </>
  );
}
