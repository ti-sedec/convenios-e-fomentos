'use client';

import * as React from 'react';
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { formatCnpj, isValidCnpj } from '@/lib/cnpj';
import {
  useFieldContext,
  useFieldInvalid,
  useFormReadOnly,
  type BaseFieldProps
} from '@/lib/form-context';

const INVALID = { message: 'CNPJ inválido.' };

function check({ value }: { value: string | undefined }) {
  return value && !isValidCnpj(value) ? INVALID : undefined;
}

/**
 * Validadores para o `form.AppField` do CNPJ: confere ao sair do campo e ao enviar, e reconfere ao digitar só depois que o
 * erro apareceu (para sumir assim que o número ficar certo, sem acusar erro no meio da digitação). Campo vazio não é erro aqui:
 * a obrigatoriedade é pendência do checklist.
 */
export const cnpjValidators = {
  onBlur: check,
  onSubmit: check,
  onChange: ({
    value,
    fieldApi
  }: {
    value: string | undefined;
    fieldApi: { state: { meta: { errorMap: { onBlur?: unknown; onSubmit?: unknown } } } };
  }) =>
    fieldApi.state.meta.errorMap.onBlur || fieldApi.state.meta.errorMap.onSubmit
      ? check({ value })
      : undefined
};

/**
 * CNPJ com máscara: 12.345.678/0001-95 (numérico) ou 12.ABC.345/01DE-35 (alfanumérico). Letras viram maiúsculas.
 * Use com `validators={cnpjValidators}` no `form.AppField`.
 */
export function CnpjField({
  label,
  description,
  required,
  ...inputProps
}: BaseFieldProps &
  Omit<React.ComponentProps<typeof Input>, 'value' | 'onChange' | 'onBlur' | 'type'>) {
  const field = useFieldContext<string>();
  const isInvalid = useFieldInvalid();
  const readOnly = useFormReadOnly();

  return (
    <Field data-invalid={isInvalid}>
      <FieldLabel htmlFor={field.name}>
        {label}
        {required && ' *'}
      </FieldLabel>
      <Input
        id={field.name}
        name={field.name}
        type='text'
        // Texto, não numérico: o CNPJ novo tem letras e o teclado numérico do celular não as teria.
        inputMode='text'
        autoComplete='off'
        autoCapitalize='characters'
        spellCheck={false}
        maxLength={18}
        placeholder='00.000.000/0000-00'
        value={field.state.value ?? ''}
        onBlur={field.handleBlur}
        onChange={(e) => field.handleChange(formatCnpj(e.target.value))}
        aria-invalid={isInvalid}
        aria-describedby={isInvalid ? `${field.name}-error` : undefined}
        {...inputProps}
        disabled={readOnly || inputProps.disabled}
      />
      {description && <FieldDescription>{description}</FieldDescription>}
      {isInvalid && <FieldError id={`${field.name}-error`} errors={field.state.meta.errors} />}
    </Field>
  );
}
