import { queryOptions, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import {
  getChecklist,
  getFilePreview,
  getDeclaration,
  getProfile,
  getProject,
  getStageHistory,
  listMembers,
  listProjects,
  type ListProjectsParams
} from './service';

export const projectKeys = {
  all: ['projects'] as const,
  lists: () => [...projectKeys.all, 'list'] as const,
  list: (params: ListProjectsParams) => [...projectKeys.lists(), params] as const,
  detail: (id: string) => [...projectKeys.all, 'detail', id] as const,
  preview: (id: string, fileId: string) => [...projectKeys.all, 'preview', id, fileId] as const,
  checklist: (id: string) => [...projectKeys.all, 'checklist', id] as const,
  history: (id: string) => [...projectKeys.all, 'history', id] as const,
  declaration: (id: string, code: string) => [...projectKeys.all, 'declaration', id, code] as const,
  members: (id: string) => [...projectKeys.all, 'members', id] as const,
  profile: () => ['proponent-profile'] as const
};

export const projectsQueryOptions = (params: ListProjectsParams = {}) =>
  queryOptions({ queryKey: projectKeys.list(params), queryFn: () => listProjects(params) });

export const projectQueryOptions = (id: string) =>
  queryOptions({ queryKey: projectKeys.detail(id), queryFn: () => getProject(id) });

/** URL de exibição do arquivo: vale por alguns minutos (STORAGE_PREVIEW_TTL), então não é guardada por mais tempo que isso. */
export const filePreviewQueryOptions = (id: string, fileId: string) =>
  queryOptions({
    queryKey: projectKeys.preview(id, fileId),
    queryFn: () => getFilePreview(id, fileId),
    staleTime: 5 * 60_000,
    gcTime: 5 * 60_000,
    retry: false
  });

export const profileQueryOptions = () =>
  queryOptions({ queryKey: projectKeys.profile(), queryFn: getProfile });

/**
 * Enquanto alguma declaração aguarda a assinatura, consulta o checklist de 5 em 5 segundos: o signatário assina no celular, e a leitura
 * do servidor conclui a assinatura e traz o arquivo para o item.
 */
export const checklistQueryOptions = (id: string) =>
  queryOptions({
    queryKey: projectKeys.checklist(id),
    queryFn: () => getChecklist(id),
    refetchInterval: (query) =>
      query.state.data?.items.some((i) => i.declarationAwaiting) ? 5000 : false
  });

export const stageHistoryQueryOptions = (id: string) =>
  queryOptions({ queryKey: projectKeys.history(id), queryFn: () => getStageHistory(id) });

export const membersQueryOptions = (id: string) =>
  queryOptions({ queryKey: projectKeys.members(id), queryFn: () => listMembers(id) });

/**
 * A declaração é sempre lida do servidor ao montar: o texto, as pendências e o resumo dependem do cadastro, que pode ter mudado em
 * outra etapa. Enquanto a assinatura está pendente, consulta de 5 em 5 segundos: o signatário assina no celular.
 */
export const declarationQueryOptions = (id: string, code: string) =>
  queryOptions({
    queryKey: projectKeys.declaration(id, code),
    queryFn: () => getDeclaration(id, code),
    staleTime: 0,
    refetchInterval: (query) =>
      query.state.data?.current?.status === 'awaiting_signature' ? 5000 : false
  });

/**
 * Depois de salvar uma seção do cadastro (ou as situações dos documentos): o checklist e o texto de cada declaração dependem dela.
 * Sem invalidar as declarações, o diálogo mostraria as pendências e o resumo antigos.
 */
export async function invalidateAfterSave(qc: QueryClient, id: string) {
  await Promise.all([
    qc.invalidateQueries({ queryKey: projectKeys.checklist(id) }),
    qc.invalidateQueries({ queryKey: [...projectKeys.all, 'declaration', id] }),
    qc.invalidateQueries({ queryKey: projectKeys.lists() })
  ]);
}

/**
 * Uma assinatura que terminou (assinada, recusada ou vencida) muda o checklist, o resumo de documentos e as pendências do projeto:
 * atualiza tudo o que depende dela.
 */
export async function invalidateAfterSignature(qc: QueryClient, id: string) {
  await Promise.all([
    qc.invalidateQueries({ queryKey: projectKeys.checklist(id) }),
    qc.invalidateQueries({ queryKey: projectKeys.detail(id) }),
    qc.invalidateQueries({ queryKey: projectKeys.lists() })
  ]);
}

/**
 * Acompanha, em qualquer etapa do projeto, as declarações que aguardam a assinatura. Quando a última deixa de aguardar, atualiza o
 * projeto: a revisão e o envio dependem do resultado e não podem ficar com o número antigo de documentos pendentes.
 * Só consulta o checklist se `enabled` (o projeto é editável por quem está na tela).
 */
export function useSignatureWatcher(id: string, enabled: boolean) {
  const qc = useQueryClient();
  const { data } = useQuery({ ...checklistQueryOptions(id), enabled });
  const awaiting = !!data?.items.some((i) => i.declarationAwaiting);
  const was = useRef(awaiting);
  useEffect(() => {
    if (was.current && !awaiting) void invalidateAfterSignature(qc, id);
    was.current = awaiting;
  }, [awaiting, qc, id]);
}
