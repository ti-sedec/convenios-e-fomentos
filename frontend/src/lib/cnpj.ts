/**
 * CNPJ: máscara e validação, espelhando backend/internal/cnpj. Aceita os dois formatos: o numérico (14 dígitos) e o alfanumérico
 * da IN RFB nº 2.229/2024 (12 posições de 0-9 e A-Z, mais 2 dígitos verificadores numéricos).
 */

const LENGTH = 14;

/** Remove a pontuação e passa as letras para maiúsculas; descarta o que não for 0-9 ou A-Z. */
export function cnpjKey(value: string): string {
  return value.replace(/[^0-9a-zA-Z]/g, '').toUpperCase();
}

/**
 * Formata enquanto digita: 12.345.678/0001-95 ou 12.ABC.345/01DE-35 (aceita CNPJ parcial). As 12 primeiras posições aceitam
 * dígitos e letras; as duas últimas (dígitos verificadores) só aceitam dígitos.
 */
export function formatCnpj(value: string): string {
  let key = '';
  for (const c of cnpjKey(value)) {
    if (key.length >= LENGTH) break;
    if (key.length >= 12 && !/\d/.test(c)) continue;
    key += c;
  }
  return (
    key.slice(0, 2) +
    (key.length > 2 ? '.' + key.slice(2, 5) : '') +
    (key.length > 5 ? '.' + key.slice(5, 8) : '') +
    (key.length > 8 ? '/' + key.slice(8, 12) : '') +
    (key.length > 12 ? '-' + key.slice(12) : '')
  );
}

/** Dígito verificador: pesos 2..9 da direita para a esquerda; o valor de cada caractere é o código ASCII menos 48 (A = 17). */
function checkDigit(base: string): number {
  let sum = 0;
  let weight = 2;
  for (let i = base.length - 1; i >= 0; i--) {
    sum += (base.charCodeAt(i) - 48) * weight;
    weight = weight === 9 ? 2 : weight + 1;
  }
  const rest = sum % 11;
  return rest < 2 ? 0 : 11 - rest;
}

/** Valida o formato e os dígitos verificadores (rejeita sequências repetidas como 11.111.111/1111-11). */
export function isValidCnpj(value: string): boolean {
  const key = cnpjKey(value);
  if (!/^[0-9A-Z]{12}\d{2}$/.test(key) || /^(.)\1{13}$/.test(key)) return false;
  return (
    checkDigit(key.slice(0, 12)) === Number(key[12]) &&
    checkDigit(key.slice(0, 13)) === Number(key[13])
  );
}
