import { queryOptions } from '@tanstack/react-query';
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

export const checklistQueryOptions = (id: string) =>
  queryOptions({ queryKey: projectKeys.checklist(id), queryFn: () => getChecklist(id) });

export const stageHistoryQueryOptions = (id: string) =>
  queryOptions({ queryKey: projectKeys.history(id), queryFn: () => getStageHistory(id) });

export const membersQueryOptions = (id: string) =>
  queryOptions({ queryKey: projectKeys.members(id), queryFn: () => listMembers(id) });

/** Enquanto a assinatura está pendente, consulta de 5 em 5 segundos: o signatário assina no celular. */
export const declarationQueryOptions = (id: string, code: string) =>
  queryOptions({
    queryKey: projectKeys.declaration(id, code),
    queryFn: () => getDeclaration(id, code),
    refetchInterval: (query) =>
      query.state.data?.current?.status === 'awaiting_signature' ? 5000 : false
  });
