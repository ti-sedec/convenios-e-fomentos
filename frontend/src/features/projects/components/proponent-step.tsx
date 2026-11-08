'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { toast } from 'sonner';
import { useStore } from '@tanstack/react-form';
import { FieldGroup } from '@/components/ui/field';
import { ReadOnlyScope } from '@/components/forms/read-only-scope';
import { useAppForm } from '@/lib/form';
import { invalidateAfterSave, projectKeys } from '../api/queries';
import { saveProponent } from '../api/service';
import type { Contact, Project } from '../api/types';
import { formatCpf, formatPhone } from '../format';
import { ContactEditor } from './contact-editor';
import { PersonalDataEditor } from './personal-data-editor';
import { ServerErrors } from './server-errors';
import { StepActions } from './step-actions';

export function contactValues(c: Contact): Contact {
  // Só os campos de Contact: o perfil e o projeto trazem também nome e CPF, que a API rejeita no corpo.
  return {
    phones: c.phones.map((p) => ({ ...p, number: formatPhone(p.number) })),
    emails: c.emails,
    uf: c.uf,
    municipalityIbge: c.municipalityIbge,
    jobTitle: c.jobTitle,
    personal: c.personal
  };
}

const representativeOptions = [
  { value: 'yes', label: 'Sim, sou o representante legal (prefeito, presidente…)' },
  { value: 'no', label: 'Não, represento a instituição a pedido do representante legal' }
];

export function ProponentStep({
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
  const p = project.proponent;

  const save = useMutation({
    mutationFn: (v: ReturnType<typeof defaults>) =>
      saveProponent(project.id, {
        version: project.version,
        ...v.contact,
        isLegalRepresentative: v.representative === '' ? null : v.representative === 'yes',
        legalRepName: v.legalRepName,
        legalRepCpf: v.legalRepCpf,
        legalRepJobTitle: v.legalRepJobTitle,
        legalRepPersonal: v.legalRepPersonal
      }),
    onSuccess: (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      void invalidateAfterSave(qc, project.id);
      toast.success('Dados do proponente salvos.');
      onSaved(saved, advance.current);
    }
  });

  const defaults = () => ({
    contact: contactValues(p),
    representative: p.isLegalRepresentative === null ? '' : p.isLegalRepresentative ? 'yes' : 'no',
    legalRepName: p.legalRepName,
    legalRepCpf: formatCpf(p.legalRepCpf),
    legalRepJobTitle: p.legalRepJobTitle,
    legalRepPersonal: p.legalRepPersonal
  });

  const form = useAppForm({
    defaultValues: defaults(),
    // Somente leitura (a equipe, ou projeto que o proponente não pode mais alterar): nada é enviado ao servidor.
    onSubmit: ({ value }) => (readOnly ? undefined : save.mutateAsync(value).catch(() => undefined))
  });
  const dirty = useStore(form.store, (s) => s.isDirty);
  useEffect(() => onDirty(dirty), [dirty, onDirty]);
  const representative = useStore(form.store, (s) => s.values.representative);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <ReadOnlyScope readOnly={readOnly}>
        <FieldGroup>
          <div className='bg-muted/40 rounded-lg border p-3 text-sm'>
            <p className='font-medium'>{p.name}</p>
            <p className='text-muted-foreground'>
              CPF {formatCpf(p.cpf)} · dados do seu login no MT Login
            </p>
          </div>
          <form.Field
            name='contact'
            children={(f) => <ContactEditor value={f.state.value} onChange={f.handleChange} />}
          />
          <form.AppField
            name='representative'
            children={(f) => (
              <f.RadioGroupField
                label='Você é o representante legal da instituição?'
                required
                options={representativeOptions}
              />
            )}
          />
          {representative === 'no' && (
            <>
              <form.AppField
                name='legalRepName'
                children={(f) => (
                  <f.TextField label='Nome do representante legal' required maxLength={150} />
                )}
              />
              <form.AppField
                name='legalRepCpf'
                children={(f) => (
                  <f.TextField
                    label='CPF do representante legal'
                    required
                    inputMode='numeric'
                    placeholder='000.000.000-00'
                  />
                )}
              />
              <form.AppField
                name='legalRepJobTitle'
                children={(f) => (
                  <f.TextField label='Cargo do representante legal' required maxLength={100} />
                )}
              />
              <fieldset className='space-y-3'>
                <legend className='text-sm leading-snug font-medium'>
                  Dados pessoais do representante legal
                </legend>
                <p className='text-muted-foreground text-sm'>
                  É ele quem assina as declarações; nacionalidade, estado civil e endereço entram no
                  texto delas.
                </p>
                <form.Field
                  name='legalRepPersonal'
                  children={(f) => (
                    <PersonalDataEditor
                      id='legal-rep-personal'
                      value={f.state.value}
                      onChange={f.handleChange}
                    />
                  )}
                />
              </fieldset>
            </>
          )}
          <ServerErrors error={save.error} />
          <StepActions
            saving={save.isPending}
            disabled={readOnly}
            onSave={() => (advance.current = false)}
            onSaveAndContinue={() => (advance.current = true)}
          />
        </FieldGroup>
      </ReadOnlyScope>
    </form>
  );
}
