'use client';

import * as React from 'react';
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field';
import { Input } from '@/components/ui/input';
import { maskPhone, maskZip } from '@/lib/masks';
import {
  useFieldContext,
  useFieldInvalid,
  useFormReadOnly,
  type BaseFieldProps
} from '@/lib/form-context';

type MaskedProps = BaseFieldProps &
  Omit<React.ComponentProps<typeof Input>, 'value' | 'onChange' | 'onBlur' | 'type'>;

/** Campo de texto que reaplica a máscara a cada digitação. A máscara é só de exibição: o servidor guarda os dígitos. */
function MaskedField({
  mask,
  label,
  description,
  required,
  ...inputProps
}: MaskedProps & { mask: (value: string) => string }) {
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
        autoComplete='off'
        value={field.state.value ?? ''}
        onBlur={field.handleBlur}
        onChange={(e) => field.handleChange(mask(e.target.value))}
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

/** Telefone com máscara: "(99) 9 9999-9999" (celular) ou "(99) 9999-9999" (fixo), conforme os dígitos. */
export function PhoneField(props: MaskedProps) {
  return (
    <MaskedField
      inputMode='tel'
      maxLength={16}
      placeholder='(99) 9 9999-9999'
      {...props}
      mask={maskPhone}
    />
  );
}

/** CEP com máscara: "99999-999". */
export function ZipField(props: MaskedProps) {
  return (
    <MaskedField
      inputMode='numeric'
      maxLength={9}
      placeholder='99999-999'
      {...props}
      mask={maskZip}
    />
  );
}
