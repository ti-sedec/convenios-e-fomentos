const MB = 1024 * 1024;

/** Espelha as regras do backend (documents/files.go); ele continua sendo a autoridade. */
export const FILE_RULES: Record<string, { exts: string[]; maxSize: number; label: string }> = {
  'application/pdf': { exts: ['.pdf'], maxSize: 200 * MB, label: 'PDF' },
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': {
    exts: ['.docx'],
    maxSize: 200 * MB,
    label: 'DOCX'
  },
  'image/png': { exts: ['.png'], maxSize: 25 * MB, label: 'PNG' },
  'image/jpeg': { exts: ['.jpg', '.jpeg'], maxSize: 25 * MB, label: 'JPEG' },
  'image/webp': { exts: ['.webp'], maxSize: 25 * MB, label: 'WebP' }
};

export const MAX_FILES_PER_DOCUMENT = 50;

export const ACCEPT_ATTR = Object.values(FILE_RULES)
  .flatMap((r) => r.exts)
  .join(',');

const EXT_TO_TYPE = new Map(
  Object.entries(FILE_RULES).flatMap(([type, r]) => r.exts.map((e) => [e, type] as const))
);

/** O tipo vem da extensão: `File.type` pode vir vazio ou errado conforme o sistema. */
export function contentTypeOf(file: File): string | null {
  const dot = file.name.lastIndexOf('.');
  if (dot < 0) return null;
  return EXT_TO_TYPE.get(file.name.slice(dot).toLowerCase()) ?? null;
}

/** Devolve a mensagem de erro, ou `null` se o arquivo pode ser enviado. */
export function validateFile(file: File): string | null {
  const type = contentTypeOf(file);
  if (!type) return 'Tipo não permitido. Use PDF, DOCX, PNG, JPEG ou WebP.';
  const { maxSize, label } = FILE_RULES[type];
  if (file.size < 1) return 'O arquivo está vazio.';
  if (file.size > maxSize) return `${label} aceita até ${maxSize / MB} MB.`;
  return null;
}
