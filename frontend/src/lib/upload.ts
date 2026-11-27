/** Envio autorizado: POST multipart para `url` com `fields` e, por último, o campo `file`. */
export interface UploadGrant {
  url: string;
  fields: Record<string, string>;
  expires_at: string;
}

export class StorageUploadError extends Error {
  constructor(
    message: string,
    readonly status?: number
  ) {
    super(message);
    this.name = 'StorageUploadError';
  }
}

/**
 * Envia o arquivo direto ao armazenamento (POST multipart autorizado pela API). Usa XMLHttpRequest
 * porque `fetch` não informa o progresso do envio. A ordem importa: o campo `file` vai por último.
 * Não leva cookies nem o Bearer: a autorização está nos campos assinados.
 */
export function uploadToStorage(
  grant: UploadGrant,
  file: File,
  onProgress: (percent: number) => void,
  signal?: AbortSignal
): Promise<void> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    for (const [k, v] of Object.entries(grant.fields)) form.append(k, v);
    form.append('file', file);

    const xhr = new XMLHttpRequest();
    xhr.open('POST', grant.url);
    xhr.upload.addEventListener('progress', (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    });
    xhr.addEventListener('load', () => {
      if (xhr.status >= 200 && xhr.status < 300) return resolve();
      reject(new StorageUploadError(storageMessage(xhr), xhr.status));
    });
    xhr.addEventListener('error', () =>
      reject(new StorageUploadError('Não foi possível enviar o arquivo ao armazenamento.'))
    );
    xhr.addEventListener('abort', () => reject(new StorageUploadError('Envio cancelado.')));
    signal?.addEventListener('abort', () => xhr.abort(), { once: true });
    xhr.send(form);
  });
}

/** O armazenamento responde XML (<Message>…</Message>); traduz os casos que o usuário pode causar. */
function storageMessage(xhr: XMLHttpRequest): string {
  const code = /<Code>([^<]+)<\/Code>/.exec(xhr.responseText)?.[1];
  switch (code) {
    case 'EntityTooLarge':
      return 'O arquivo excede o tamanho permitido.';
    case 'EntityTooSmall':
      return 'O arquivo está vazio.';
    case 'AccessDenied':
      return 'A autorização do envio expirou ou foi recusada. Tente novamente.';
    default:
      return `O armazenamento recusou o envio (${xhr.status}).`;
  }
}
