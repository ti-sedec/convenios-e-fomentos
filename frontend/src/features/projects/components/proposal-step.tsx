'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { toast } from 'sonner';
import { useStore } from '@tanstack/react-form';
import { FieldGroup } from '@/components/ui/field';
import { ReadOnlyScope } from '@/components/forms/read-only-scope';
import { useAppForm } from '@/lib/form';
import { invalidateAfterSave, projectKeys } from '../api/queries';
import { saveProposal } from '../api/service';
import { objectNatureLabels, type ObjectNature, type Project } from '../api/types';
import { ExecutionPeriodsEditor, fromRow, rowsOf } from './execution-periods-editor';
import { ServerErrors } from './server-errors';
import { StepActions } from './step-actions';

const natureOptions = Object.entries(objectNatureLabels).map(([value, label]) => ({
  value,
  label
}));

export function ProposalStep({
  project,
  readOnly,
  onSaved,
  onDirty
}: {
  project: Project;
  readOnly: boolean;
  onSaved: (project: Project, advance: boolean) => void;
  onDirty: (dirty: boolean) => void;
}) {
  const qc = useQueryClient();
  const advance = useRef(false);
  const p = project.proposal;

  const save = useMutation({
    mutationFn: (v: ReturnType<typeof defaults>) =>
      saveProposal(project.id, {
        version: project.version,
        ...v,
        objectNature: v.objectNature as ObjectNature | '',
        executionPeriods: v.executionPeriods.map(fromRow)
      }),
    onSuccess: (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      void invalidateAfterSave(qc, project.id);
      toast.success('Proposta salva.');
      onSaved(saved, advance.current);
    }
  });

  const defaults = () => ({
    sigconNumber: p.sigconNumber,
    sigconRegisteredOn: p.sigconRegisteredOn,
    title: p.title,
    object: p.object,
    justification: p.justification,
    objectNature: p.objectNature as string,
    hasArtisticHiring: p.hasArtisticHiring,
    calamity: p.calamity,
    executionPeriods: rowsOf(p.executionPeriods)
  });

  const form = useAppForm({
    defaultValues: defaults(),
    // Somente leitura (a equipe, ou projeto que o proponente não pode mais alterar): nada é enviado ao servidor.
    onSubmit: ({ value }) => (readOnly ? undefined : save.mutateAsync(value).catch(() => undefined))
  });
  const dirty = useStore(form.store, (s) => s.isDirty);
  useEffect(() => onDirty(dirty), [dirty, onDirty]);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <ReadOnlyScope readOnly={readOnly}>
        <FieldGroup>
          <div className='grid gap-4 sm:grid-cols-2'>
            <form.AppField
              name='sigconNumber'
              children={(f) => (
                <f.TextField
                  label='Nº da proposta no SIGCon'
                  required
                  placeholder='3216/2026'
                  description='Número da proposta/termo, no formato NNNN/AAAA.'
                />
              )}
            />
            <form.AppField
              name='sigconRegisteredOn'
              children={(f) => (
                <f.TextField label='Data do cadastro no SIGCon' type='date' required />
              )}
            />
          </div>
          <form.AppField
            name='title'
            children={(f) => <f.TextField label='Título do projeto' required maxLength={200} />}
          />
          <form.AppField
            name='object'
            children={(f) => (
              <f.TextareaField
                label='Objeto'
                required
                rows={4}
                maxLength={5000}
                showCount
                description='O que será feito, em linguagem clara.'
              />
            )}
          />
          <form.AppField
            name='justification'
            children={(f) => (
              <f.TextareaField
                label='Justificativa'
                required
                rows={4}
                maxLength={10000}
                showCount
              />
            )}
          />
          <form.Field
            name='executionPeriods'
            children={(f) => (
              <ExecutionPeriodsEditor value={f.state.value} onChange={f.handleChange} />
            )}
          />
          <form.AppField
            name='objectNature'
            children={(f) => (
              <f.SelectField
                label='Natureza do objeto'
                required
                placeholder='Escolha'
                options={natureOptions}
                description='Define quais documentos de engenharia, licenças e regras específicas se aplicam.'
              />
            )}
          />
          <form.AppField
            name='hasArtisticHiring'
            children={(f) => (
              <f.SwitchField
                label='Haverá contratação de artistas (shows e eventos musicais)?'
                description='Aplica os limites da Lei Estadual 12.082/2023.'
              />
            )}
          />
          <form.AppField
            name='calamity'
            children={(f) => (
              <f.SwitchField
                label='Calamidade pública ou situação de emergência?'
                description='Dispensa a contrapartida, mediante o ato oficial.'
              />
            )}
          />
          <ServerErrors error={save.error} />
          <StepActions
            saving={save.isPending}
            disabled={readOnly}
            onSave={() => (advance.current = false)}
            onSaveAndContinue={() => (advance.current = true)}
            last
          />
        </FieldGroup>
      </ReadOnlyScope>
    </form>
  );
}
