'use client';

import { useEffect, useRef } from 'react';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { UFS } from '@/features/municipalities/api/queries';
import { useZipLookup } from '@/features/zip-codes/use-zip-lookup';
import { maskZip } from '@/lib/masks';
import type { MaritalStatus, PersonalData } from '../api/types';
import { MunicipalitySelect } from './municipality-select';

export const emptyPersonal: PersonalData = {
  nationality: '',
  maritalStatus: '',
  zip: '',
  street: '',
  number: '',
  complement: '',
  district: '',
  uf: '',
  municipalityIbge: ''
};

const maritalOptions: { value: MaritalStatus; label: string }[] = [
  { value: 'single', label: 'Solteiro(a)' },
  { value: 'married', label: 'Casado(a)' },
  { value: 'stable_union', label: 'Em união estável' },
  { value: 'separated', label: 'Separado(a)' },
  { value: 'divorced', label: 'Divorciado(a)' },
  { value: 'widowed', label: 'Viúvo(a)' }
];

/**
 * Dados pessoais de quem assina as declarações: nacionalidade, estado civil e endereço residencial. O CPF já identifica a pessoa,
 * então não se pede RG. id distingue os campos quando há mais de um editor na página.
 */
export function PersonalDataEditor({
  id,
  value,
  onChange
}: {
  id: string;
  value: PersonalData;
  onChange: (next: PersonalData) => void;
}) {
  const patch = (p: Partial<PersonalData>) => onChange({ ...value, ...p });
  // A consulta demora: aplica sobre o valor mais recente, para não desfazer o que a pessoa digitou enquanto esperava.
  const latest = useRef(value);
  useEffect(() => {
    latest.current = value;
  });
  const zipLookup = useZipLookup((a) =>
    onChange({
      ...latest.current,
      street: a.street || latest.current.street,
      district: a.district || latest.current.district,
      uf: a.uf || latest.current.uf,
      municipalityIbge: a.ibgeCode || latest.current.municipalityIbge
    })
  );
  return (
    <FieldGroup>
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field>
          <FieldLabel htmlFor={`${id}-nationality`}>Nacionalidade *</FieldLabel>
          <Input
            id={`${id}-nationality`}
            maxLength={60}
            value={value.nationality}
            onChange={(e) => patch({ nationality: e.target.value })}
            placeholder='brasileiro(a)'
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-marital`}>Estado civil *</FieldLabel>
          <NativeSelect
            id={`${id}-marital`}
            className='w-full'
            value={value.maritalStatus}
            onChange={(e) => patch({ maritalStatus: e.target.value as MaritalStatus | '' })}
          >
            <NativeSelectOption value=''>—</NativeSelectOption>
            {maritalOptions.map((o) => (
              <NativeSelectOption key={o.value} value={o.value}>
                {o.label}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
      </div>
      <p className='text-sm font-medium'>Endereço residencial</p>
      <div className='grid gap-4 sm:grid-cols-[9rem_1fr_7rem]'>
        <Field>
          <FieldLabel htmlFor={`${id}-zip`}>CEP</FieldLabel>
          <Input
            id={`${id}-zip`}
            inputMode='numeric'
            maxLength={9}
            placeholder='99999-999'
            value={value.zip}
            onChange={(e) => patch({ zip: maskZip(e.target.value) })}
            onBlur={(e) => void zipLookup.lookup(e.target.value)}
          />
          {zipLookup.loading && (
            <FieldDescription className='flex items-center gap-1'>
              <Spinner className='size-3' /> Buscando…
            </FieldDescription>
          )}
          {zipLookup.message && <FieldDescription>{zipLookup.message}</FieldDescription>}
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-street`}>Rua, avenida… *</FieldLabel>
          <Input
            id={`${id}-street`}
            maxLength={150}
            value={value.street}
            onChange={(e) => patch({ street: e.target.value })}
            placeholder='Rua das Flores'
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-number`}>Número *</FieldLabel>
          <Input
            id={`${id}-number`}
            maxLength={20}
            value={value.number}
            onChange={(e) => patch({ number: e.target.value })}
            placeholder='S/N'
          />
        </Field>
      </div>
      <div className='grid gap-4 sm:grid-cols-2'>
        <Field>
          <FieldLabel htmlFor={`${id}-complement`}>Complemento</FieldLabel>
          <Input
            id={`${id}-complement`}
            maxLength={100}
            value={value.complement}
            onChange={(e) => patch({ complement: e.target.value })}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-district`}>Bairro *</FieldLabel>
          <Input
            id={`${id}-district`}
            maxLength={100}
            value={value.district}
            onChange={(e) => patch({ district: e.target.value })}
          />
        </Field>
      </div>
      <div className='grid gap-4 sm:grid-cols-[8rem_1fr]'>
        <Field>
          <FieldLabel htmlFor={`${id}-uf`}>UF</FieldLabel>
          <NativeSelect
            id={`${id}-uf`}
            className='w-full'
            value={value.uf}
            onChange={(e) => patch({ uf: e.target.value, municipalityIbge: '' })}
          >
            <NativeSelectOption value=''>—</NativeSelectOption>
            {UFS.map((uf) => (
              <NativeSelectOption key={uf} value={uf}>
                {uf}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field>
          <FieldLabel htmlFor={`${id}-municipality`}>Município *</FieldLabel>
          <MunicipalitySelect
            id={`${id}-municipality`}
            uf={value.uf}
            value={value.municipalityIbge}
            onChange={(municipalityIbge) => patch({ municipalityIbge })}
          />
        </Field>
      </div>
      <FieldDescription>
        Os campos com * aparecem no texto das declarações de gratuidade e de publicidade, assinadas
        por esta pessoa, e são necessários para gerá-las (no número, use S/N se não houver). Não
        impedem salvar o cadastro.
      </FieldDescription>
    </FieldGroup>
  );
}
