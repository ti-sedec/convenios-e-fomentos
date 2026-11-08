'use client';

import { Icons } from '@/components/icons';
import { Button } from '@/components/ui/button';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { UFS } from '@/features/municipalities/api/queries';
import { useFormReadOnly } from '@/lib/form-context';
import { maskPhone } from '@/lib/masks';
import type { Contact, Email, Phone } from '../api/types';
import { MunicipalitySelect } from './municipality-select';
import { PersonalDataEditor } from './personal-data-editor';

const MAX = 5;

function PhonesEditor({ value, onChange }: { value: Phone[]; onChange: (next: Phone[]) => void }) {
  const readOnly = useFormReadOnly();
  const set = (i: number, patch: Partial<Phone>) =>
    onChange(value.map((p, idx) => (idx === i ? { ...p, ...patch } : p)));
  return (
    <fieldset className='space-y-2'>
      <legend className='text-sm leading-snug font-medium'>Telefones *</legend>
      <p className='text-muted-foreground text-sm'>Informe pelo menos um celular (com DDD).</p>
      {value.map((p, i) => (
        <div key={i} className='flex flex-wrap items-center gap-2'>
          <NativeSelect
            aria-label={`Tipo do telefone ${i + 1}`}
            value={p.type}
            onChange={(e) => set(i, { type: e.target.value as Phone['type'] })}
          >
            <NativeSelectOption value='mobile'>Celular</NativeSelectOption>
            <NativeSelectOption value='landline'>Fixo</NativeSelectOption>
          </NativeSelect>
          <Input
            aria-label={`Número do telefone ${i + 1}`}
            inputMode='tel'
            className='w-48'
            maxLength={16}
            placeholder={p.type === 'mobile' ? '(99) 9 9999-9999' : '(99) 9999-9999'}
            value={p.number}
            onChange={(e) => set(i, { number: maskPhone(e.target.value) })}
          />
          {!readOnly && (
            <Button
              type='button'
              size='icon'
              variant='ghost'
              aria-label={`Remover telefone ${i + 1}`}
              onClick={() => onChange(value.filter((_, idx) => idx !== i))}
            >
              <Icons.trash aria-hidden />
            </Button>
          )}
        </div>
      ))}
      {!readOnly && value.length < MAX && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => onChange([...value, { number: '', type: 'mobile' }])}
        >
          <Icons.add aria-hidden /> Adicionar telefone
        </Button>
      )}
    </fieldset>
  );
}

function EmailsEditor({ value, onChange }: { value: Email[]; onChange: (next: Email[]) => void }) {
  const readOnly = useFormReadOnly();
  const set = (i: number, address: string) =>
    onChange(value.map((e, idx) => (idx === i ? { ...e, address } : e)));
  const makePrimary = (i: number) =>
    onChange(value.map((e, idx) => ({ ...e, primary: idx === i })));
  return (
    <fieldset className='space-y-2'>
      <legend className='text-sm leading-snug font-medium'>E-mails *</legend>
      <p className='text-muted-foreground text-sm'>
        Informe pelo menos um. As comunicações do projeto vão para o principal.
      </p>
      {value.map((e, i) => (
        <div key={i} className='flex flex-wrap items-center gap-2'>
          <Input
            aria-label={`E-mail ${i + 1}`}
            type='email'
            className='w-72'
            placeholder='nome@exemplo.com'
            value={e.address}
            onChange={(ev) => set(i, ev.target.value)}
          />
          <label className='flex items-center gap-1.5 text-sm'>
            <input
              type='radio'
              name='primary-email'
              checked={e.primary}
              onChange={() => makePrimary(i)}
            />
            Principal
          </label>
          {!readOnly && (
            <Button
              type='button'
              size='icon'
              variant='ghost'
              aria-label={`Remover e-mail ${i + 1}`}
              onClick={() => onChange(value.filter((_, idx) => idx !== i))}
            >
              <Icons.trash aria-hidden />
            </Button>
          )}
        </div>
      ))}
      {!readOnly && value.length < MAX && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => onChange([...value, { address: '', primary: value.length === 0 }])}
        >
          <Icons.add aria-hidden /> Adicionar e-mail
        </Button>
      )}
    </fieldset>
  );
}

/** Edita os dados de contato do proponente: telefones, e-mails, município e cargo. */
export function ContactEditor({
  value,
  onChange
}: {
  value: Contact;
  onChange: (next: Contact) => void;
}) {
  const patch = (p: Partial<Contact>) => onChange({ ...value, ...p });
  return (
    <FieldGroup>
      <PhonesEditor value={value.phones} onChange={(phones) => patch({ phones })} />
      <EmailsEditor value={value.emails} onChange={(emails) => patch({ emails })} />
      <div className='grid gap-4 sm:grid-cols-[8rem_1fr]'>
        <Field>
          <FieldLabel htmlFor='contact-uf'>UF *</FieldLabel>
          <NativeSelect
            id='contact-uf'
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
          <FieldLabel htmlFor='contact-municipality'>Município *</FieldLabel>
          <MunicipalitySelect
            id='contact-municipality'
            uf={value.uf}
            value={value.municipalityIbge}
            onChange={(municipalityIbge) => patch({ municipalityIbge })}
          />
        </Field>
      </div>
      <Field>
        <FieldLabel htmlFor='contact-job'>Cargo na instituição *</FieldLabel>
        <Input
          id='contact-job'
          maxLength={100}
          value={value.jobTitle}
          onChange={(e) => patch({ jobTitle: e.target.value })}
          placeholder='Prefeito, Presidente, Secretário…'
        />
        <FieldDescription>Até 100 caracteres.</FieldDescription>
      </Field>
      <fieldset className='space-y-3'>
        <legend className='text-sm leading-snug font-medium'>Seus dados pessoais</legend>
        <p className='text-muted-foreground text-sm'>
          Usados nas declarações que você assina. Se o representante legal for outra pessoa, os
          dados dele são pedidos mais adiante.
        </p>
        <PersonalDataEditor
          id='contact-personal'
          value={value.personal}
          onChange={(personal) => patch({ personal })}
        />
      </fieldset>
    </FieldGroup>
  );
}
