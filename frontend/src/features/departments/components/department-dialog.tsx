'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import * as z from 'zod';
import { Icons } from '@/components/icons';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/ui/dialog';
import { FieldGroup } from '@/components/ui/field';
import { useCan } from '@/features/auth/session-provider';
import { errorMessage } from '@/lib/errors';
import { useAppForm } from '@/lib/form';
import { departmentKeys } from '../api/queries';
import { createDepartment, updateDepartment, type DepartmentInput } from '../api/service';
import { departmentActions, type Department } from '../api/types';

const schema = z.object({
  name: z.string().trim().min(1, 'Informe o nome.').max(100, 'Máximo de 100 caracteres.'),
  isFinalistic: z.boolean()
});

function DepartmentForm({ department, onDone }: { department?: Department; onDone: () => void }) {
  const qc = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const save = useMutation({
    mutationFn: (input: DepartmentInput) =>
      department ? updateDepartment(department.id, input) : createDepartment(input),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: departmentKeys.all });
      toast.success(department ? 'Departamento atualizado.' : 'Departamento criado.');
      onDone();
    },
    onError: (err) => setError(errorMessage(err))
  });
  const form = useAppForm({
    defaultValues: {
      name: department?.name ?? '',
      isFinalistic: department?.isFinalistic ?? false
    },
    validators: { onSubmit: schema },
    onSubmit: ({ value }) => {
      setError(null);
      return save.mutateAsync({ name: value.name.trim(), isFinalistic: value.isFinalistic });
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
        <form.AppField name='name' children={(f) => <f.TextField label='Nome' required />} />
        <form.AppField
          name='isFinalistic'
          children={(f) => (
            <f.SwitchField
              label='Área finalística'
              description='Só departamentos marcados como área finalística podem ser indicados para enquadrar um projeto.'
            />
          )}
        />
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        <form.AppForm>
          <form.SubmitButton>{department ? 'Salvar' : 'Criar departamento'}</form.SubmitButton>
        </form.AppForm>
      </FieldGroup>
    </form>
  );
}

/** Sem `department`, é o botão "Novo departamento"; com ele, o diálogo (controlado) de edição. */
export function DepartmentDialog(
  props:
    | { department: Department; open: boolean; onOpenChange: (open: boolean) => void }
    | {
        department?: undefined;
        open?: undefined;
        onOpenChange?: undefined;
      }
) {
  const can = useCan();
  const [open, setOpen] = useState(false);

  if (props.department) {
    return (
      <Dialog open={props.open} onOpenChange={props.onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Editar departamento</DialogTitle>
            <DialogDescription>
              Administradores e projetos vinculados continuam nele. Desmarcar a área finalística só
              afeta indicações novas.
            </DialogDescription>
          </DialogHeader>
          <DepartmentForm department={props.department} onDone={() => props.onOpenChange(false)} />
        </DialogContent>
      </Dialog>
    );
  }
  if (!can(departmentActions.create)) return null;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button />}>
        <Icons.add aria-hidden /> Novo departamento
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Novo departamento</DialogTitle>
          <DialogDescription>
            Departamentos delimitam o alcance das permissões com a condição &quot;mesmo
            departamento&quot;.
          </DialogDescription>
        </DialogHeader>
        <DepartmentForm onDone={() => setOpen(false)} />
      </DialogContent>
    </Dialog>
  );
}
