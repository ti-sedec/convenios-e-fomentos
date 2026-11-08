const digitsOf = (s: string) => s.replace(/\D/g, '');

/**
 * Máscara de telefone aplicada enquanto a pessoa digita. O formato depende da quantidade de dígitos (DDD + número):
 * 11 dígitos (celular) → "(99) 9 9999-9999"; até 10 (fixo) → "(99) 9999-9999". Não acrescenta separador no fim, então o
 * Backspace apaga um dígito por vez.
 */
export function maskPhone(value: string): string {
  let d = digitsOf(value);
  // Número colado com o código do país (+55 …): descarta o 55.
  if (value.trim().startsWith('+55') || (d.length === 13 && d.startsWith('55'))) d = d.slice(2);
  d = d.slice(0, 11);
  if (d.length === 0) return '';
  if (d.length <= 2) return `(${d}`;
  const ddd = d.slice(0, 2);
  const rest = d.slice(2);
  if (d.length === 11) return `(${ddd}) ${rest[0]} ${rest.slice(1, 5)}-${rest.slice(5)}`;
  return rest.length > 4 ? `(${ddd}) ${rest.slice(0, 4)}-${rest.slice(4)}` : `(${ddd}) ${rest}`;
}

/** Máscara de CEP aplicada enquanto a pessoa digita: "99999-999". */
export function maskZip(value: string): string {
  const d = digitsOf(value).slice(0, 8);
  return d.length > 5 ? `${d.slice(0, 5)}-${d.slice(5)}` : d;
}
