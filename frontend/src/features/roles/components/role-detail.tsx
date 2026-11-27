'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { AlertModal } from '@/components/modal/alert-modal';
import { QueryError } from '@/components/query-error';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button, buttonVariants } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { FieldGroup } from '@/components/ui/field';
import { Skeleton } from '@/components/ui/skeleton';
import { auditActions } from '@/features/audit-logs/api/types';
import { AuditHistory } from '@/features/audit-logs/components/audit-history';
import { useCan } from '@/features/auth/session-provider';
import { ApiError } from '@/lib/api/errors';
import { errorMessage } from '@/lib/errors';
import { useAppForm } from '@/lib/form';
import {
  catalogActionsQueryOptions,
  catalogConditionsQueryOptions,
  roleKeys,
  roleQueryOptions
} from '../api/queries';
import { deleteRole, updateRole } from '../api/service';
import { roleActions, type RoleDetail as RoleDetailData } from '../api/types';
import { PermissionEditor } from './permission-editor';
import { roleSchema } from './new-role-dialog';

function RoleForm({ role, editable }: { role: RoleDetailData; editable: boolean }) {
  const qc = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: (v: { name: string; description: string }) => updateRole(role.id, v),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: roleKeys.all });
      toast.success('Perfil atualizado.');
    },
    onError: (err) => setError(errorMessage(err))
  });
  const form = useAppForm({
    defaultValues: { name: role.name, description: role.description },
    validators: { onSubmit: roleSchema },
    onSubmit: ({ value }) => {
      setError(null);
      return save.mutateAsync({ name: value.name.trim(), description: value.description });
    }
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.AppField
          name='name'
          children={(f) => <f.TextField label='Nome' required disabled={!editable} />}
        />
        <form.AppField
          name='description'
          children={(f) => <f.TextareaField label='Descrição' disabled={!editable} />}
        />
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        {editable && (
          <form.AppForm>
            <form.SubmitButton>Salvar</form.SubmitButton>
          </form.AppForm>
        )}
      </FieldGroup>
    </form>
  );
}

export function RoleDetail({ id }: { id: string }) {
  const can = useCan();
  const router = useRouter();
  const qc = useQueryClient();
  const role = useQuery(roleQueryOptions(id));
  const catalog = useQuery(catalogActionsQueryOptions());
  const conditions = useQuery(catalogConditionsQueryOptions());
  const [confirmDelete, setConfirmDelete] = useState(false);

  const remove = useMutation({
    mutationFn: () => deleteRole(id),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: roleKeys.all });
      toast.success('Perfil excluído.');
      router.push('/dashboard/administracao/perfis');
    },
    onError: (err) => {
      toast.error(errorMessage(err));
      setConfirmDelete(false);
    }
  });

  if (role.isLoading) return <Skeleton className='h-40 w-full max-w-4xl' />;
  if (role.isError || !role.data) {
    return role.error instanceof ApiError && role.error.status === 404 ? (
      <Alert variant='destructive' className='max-w-4xl'>
        <AlertDescription>Perfil não encontrado.</AlertDescription>
      </Alert>
    ) : (
      <QueryError error={role.error} what='o perfil' />
    );
  }

  const r = role.data;
  const editable = can(roleActions.update) && !r.isSuperAdmin;

  return (
    <div className='grid max-w-4xl gap-4'>
      <div>
        <Link
          href='/dashboard/administracao/perfis'
          className={buttonVariants({ variant: 'ghost', size: 'sm' })}
        >
          <Icons.chevronLeft aria-hidden /> Perfis
        </Link>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className='flex flex-wrap items-center gap-2'>
            {r.name}
            {r.isSuperAdmin ? (
              <Badge>Acesso total</Badge>
            ) : (
              r.isSystem && <Badge variant='secondary'>Sistema</Badge>
            )}
          </CardTitle>
          <CardDescription>
            <Link
              href={`/dashboard/administracao/usuarios?roleId=${r.id}`}
              className='hover:underline'
            >
              {r.userCount === 1 ? '1 administrador tem' : `${r.userCount} administradores têm`}{' '}
              este perfil
            </Link>
          </CardDescription>
        </CardHeader>
        <CardContent className='grid gap-4'>
          <RoleForm key={r.updatedAt} role={r} editable={editable} />
          {can(roleActions.delete) && !r.isSuperAdmin && (
            <div>
              <Button variant='destructive' onClick={() => setConfirmDelete(true)}>
                <Icons.trash aria-hidden /> Excluir perfil
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Permissões</CardTitle>
          <CardDescription>
            Marque o que o perfil pode fazer. Ações com campos ou condições podem ser restringidas.
            Quem não é Super Admin só concede permissões que possui.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {catalog.isLoading || conditions.isLoading ? (
            <Skeleton className='h-40 w-full' />
          ) : catalog.isError || conditions.isError || !catalog.data || !conditions.data ? (
            <QueryError error={catalog.error ?? conditions.error} what='o catálogo de permissões' />
          ) : (
            <PermissionEditor
              // Remonta ao trocar de perfil ou após um salvamento (estado novo vindo do servidor).
              key={`${r.id}-${r.updatedAt}`}
              role={r}
              catalog={catalog.data}
              conditions={conditions.data}
            />
          )}
        </CardContent>
      </Card>

      {can(auditActions.read) && (
        <Card>
          <CardHeader>
            <CardTitle>Histórico</CardTitle>
            <CardDescription>Últimas alterações neste perfil.</CardDescription>
          </CardHeader>
          <CardContent>
            <AuditHistory entity='admin_role' entityId={r.id} />
          </CardContent>
        </Card>
      )}

      <AlertModal
        isOpen={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        onConfirm={() => remove.mutate()}
        loading={remove.isPending}
        title={`Excluir o perfil "${r.name}"?`}
        description='O perfil e suas permissões serão removidos. Perfis ainda atribuídos a administradores não podem ser excluídos.'
        confirmLabel='Excluir'
        cancelLabel='Cancelar'
      />
    </div>
  );
}
