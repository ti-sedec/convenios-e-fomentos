'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { toast } from 'sonner';
import { useStore } from '@tanstack/react-form';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field';
import { entityTypesQueryOptions } from '@/features/entity-types/api/queries';
import { UFS } from '@/features/municipalities/api/queries';
import { useZipLookup } from '@/features/zip-codes/use-zip-lookup';
import { cnpjValidators } from '@/components/forms/fields/cnpj-field';
import { ReadOnlyScope } from '@/components/forms/read-only-scope';
import { useAppForm } from '@/lib/form';
import { invalidateAfterSave, projectKeys } from '../api/queries';
import { saveInstitution } from '../api/service';
import type { Project } from '../api/types';
import { formatCnpj, formatPhone, formatZip } from '../format';
import { MunicipalitySelect } from './municipality-select';
import { ServerErrors } from './server-errors';
import { StepActions } from './step-actions';

const instrumentOptions = [
  { value: 'convenio', label: 'Convênio' },
  { value: 'fomento', label: 'Termo de Fomento' }
];
const natureOptions = [
  { value: 'public', label: 'Pública' },
  { value: 'private', label: 'Privada' }
];
const habilitationOptions = [
  { value: 'full', label: 'Plena' },
  { value: 'partial', label: 'Parcial' }
];
const ufOptions = UFS.map((uf) => ({ value: uf, label: uf }));

export function InstitutionStep({
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
  const i = project.institution;
  const types = useQuery(entityTypesQueryOptions());

  const save = useMutation({
    mutationFn: (v: ReturnType<typeof defaults>) =>
      saveInstitution(project.id, {
        version: project.version,
        ...v,
        instrument: v.instrument as typeof i.instrument,
        nature: v.nature as typeof i.nature,
        habilitationStatus: v.habilitationStatus as typeof i.habilitationStatus
      }),
    onSuccess: (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      void invalidateAfterSave(qc, project.id);
      toast.success('Dados da instituição salvos.');
      onSaved(saved, advance.current);
    }
  });

  const defaults = () => ({
    instrument: i.instrument as string,
    cnpj: formatCnpj(i.cnpj),
    name: i.name,
    entityTypeId: i.entityTypeId,
    entityTypeOther: i.entityTypeOther,
    nature: i.nature as string,
    nonProfit: i.nonProfit,
    uf: i.uf,
    municipalityIbge: i.municipalityIbge,
    zip: formatZip(i.zip),
    street: i.street,
    number: i.number,
    complement: i.complement,
    district: i.district,
    email: i.email,
    phone: formatPhone(i.phone),
    habilitationStatus: i.habilitationStatus as string,
    habilitationNumber: i.habilitationNumber,
    habilitationValidUntil: i.habilitationValidUntil
  });

  const form = useAppForm({
    defaultValues: defaults(),
    // Somente leitura (a equipe, ou projeto que o proponente não pode mais alterar): nada é enviado ao servidor.
    onSubmit: ({ value }) => (readOnly ? undefined : save.mutateAsync(value).catch(() => undefined))
  });
  const dirty = useStore(form.store, (s) => s.isDirty);
  useEffect(() => onDirty(dirty), [dirty, onDirty]);
  const values = useStore(form.store, (s) => s.values);
  // Lê e grava no formulário na hora de aplicar, então vale o que a pessoa digitou enquanto a consulta rodava.
  const zipLookup = useZipLookup((a) => {
    const set = (name: 'street' | 'district' | 'uf' | 'municipalityIbge', v: string) => {
      if (v) form.setFieldValue(name, v);
    };
    set('street', a.street);
    set('district', a.district);
    set('uf', a.uf);
    set('municipalityIbge', a.ibgeCode);
  });

  // Um tipo desativado depois da escolha continua aparecendo no projeto que já o usa.
  const options = (types.data ?? [])
    .filter((t) => t.active || t.id === i.entityTypeId)
    .map((t) => ({ value: t.id, label: t.name }));
  const chosen = types.data?.find((t) => t.id === values.entityTypeId);
  const isOther = chosen?.isOther ?? false;
  const nature = isOther ? values.nature : (chosen?.nature ?? '');
  const nonProfit = isOther ? values.nonProfit : (chosen?.nonProfit ?? false);
  const fomentoBlocked =
    values.instrument === 'fomento' && nature !== '' && !(nature === 'private' && nonProfit);

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <ReadOnlyScope readOnly={readOnly}>
        <FieldGroup>
          <form.AppField
            name='instrument'
            children={(f) => (
              <f.RadioGroupField
                label='Instrumento'
                required
                description='Convênio: parceria com órgão ou entidade pública (também aceito para entidade privada). Termo de Fomento: somente para organização da sociedade civil sem fins lucrativos.'
                options={instrumentOptions}
              />
            )}
          />
          <form.AppField
            name='cnpj'
            validators={cnpjValidators}
            children={(f) => (
              <f.CnpjField
                label='CNPJ'
                required
                description='Aceita o CNPJ numérico e o alfanumérico (com letras).'
              />
            )}
          />
          <form.AppField
            name='name'
            children={(f) => (
              <f.TextField label='Nome do órgão ou instituição' required maxLength={200} />
            )}
          />
          <form.AppField
            name='entityTypeId'
            children={(f) => (
              <f.SelectField
                label='Tipo de entidade'
                required
                placeholder='Escolha o tipo'
                options={options}
              />
            )}
          />
          {isOther && (
            <>
              <form.AppField
                name='entityTypeOther'
                children={(f) => (
                  <f.TextField
                    label='Qual o tipo de entidade?'
                    required
                    maxLength={100}
                    description='Descreva por extenso.'
                  />
                )}
              />
              <form.AppField
                name='nature'
                children={(f) => (
                  <f.SelectField
                    label='Natureza'
                    required
                    placeholder='Pública ou privada'
                    options={natureOptions}
                  />
                )}
              />
              {values.nature === 'private' && (
                <form.AppField
                  name='nonProfit'
                  children={(f) => (
                    <f.SwitchField
                      label='Sem fins lucrativos'
                      description='Necessário para o Termo de Fomento.'
                    />
                  )}
                />
              )}
            </>
          )}
          {chosen && !isOther && (
            <p className='text-muted-foreground text-sm'>
              Natureza:{' '}
              {chosen.nature === 'public'
                ? 'pública'
                : chosen.nonProfit
                  ? 'privada, sem fins lucrativos'
                  : 'privada'}
              .
            </p>
          )}
          {fomentoBlocked && (
            <Alert variant='destructive'>
              <AlertDescription>
                O Termo de Fomento só pode ser firmado por entidade privada sem fins lucrativos.
                Para esta entidade, escolha Convênio.
              </AlertDescription>
            </Alert>
          )}

          <div className='grid gap-4 sm:grid-cols-[8rem_1fr]'>
            <form.AppField
              name='uf'
              children={(f) => (
                <f.SelectField label='UF da sede' required placeholder='UF' options={ufOptions} />
              )}
            />
            <form.Field
              name='municipalityIbge'
              children={(f) => (
                <Field>
                  <FieldLabel htmlFor='institution-municipality'>Município da sede *</FieldLabel>
                  <MunicipalitySelect
                    id='institution-municipality'
                    uf={values.uf}
                    value={f.state.value}
                    onChange={f.handleChange}
                  />
                  <FieldDescription>
                    Pode ser de qualquer estado; a execução do projeto é que precisa ser em Mato
                    Grosso.
                  </FieldDescription>
                </Field>
              )}
            />
          </div>
          <div className='grid gap-4 sm:grid-cols-[10rem_1fr_8rem]'>
            <div onBlur={(e) => void zipLookup.lookup((e.target as HTMLInputElement).value)}>
              <form.AppField
                name='zip'
                children={(f) => (
                  <f.ZipField
                    label='CEP'
                    required
                    description={zipLookup.loading ? 'Buscando…' : zipLookup.message || undefined}
                  />
                )}
              />
            </div>
            <form.AppField
              name='street'
              children={(f) => <f.TextField label='Logradouro' required maxLength={150} />}
            />
            <form.AppField
              name='number'
              children={(f) => <f.TextField label='Número' required maxLength={20} />}
            />
          </div>
          <div className='grid gap-4 sm:grid-cols-2'>
            <form.AppField
              name='complement'
              children={(f) => <f.TextField label='Complemento' maxLength={100} />}
            />
            <form.AppField
              name='district'
              children={(f) => <f.TextField label='Bairro' required maxLength={100} />}
            />
          </div>
          <div className='grid gap-4 sm:grid-cols-2'>
            <form.AppField
              name='email'
              children={(f) => <f.TextField label='E-mail institucional' type='email' required />}
            />
            <form.AppField
              name='phone'
              children={(f) => <f.PhoneField label='Telefone institucional' required />}
            />
          </div>

          <fieldset className='space-y-4 rounded-lg border p-4'>
            <legend className='px-1 text-sm font-medium'>Certidão de Habilitação do SIGCon</legend>
            <p className='text-muted-foreground text-sm'>
              A habilitação é feita na SEFAZ, pelo SIFI/MT. Para dar entrada é preciso ter a
              Certidão de Habilitação <strong>Plena</strong>; o arquivo da certidão é anexado na
              etapa de documentos.
            </p>
            <div className='grid gap-4 sm:grid-cols-3'>
              <form.AppField
                name='habilitationStatus'
                children={(f) => (
                  <f.SelectField
                    label='Situação'
                    required
                    placeholder='Plena ou parcial'
                    options={habilitationOptions}
                  />
                )}
              />
              <form.AppField
                name='habilitationNumber'
                children={(f) => <f.TextField label='Número da certidão' required maxLength={50} />}
              />
              <form.AppField
                name='habilitationValidUntil'
                children={(f) => <f.TextField label='Válida até' type='date' required />}
              />
            </div>
          </fieldset>

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
