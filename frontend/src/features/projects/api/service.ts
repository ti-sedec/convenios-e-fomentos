import { apiClient } from '@/lib/api-client';
import { apiErrorFromResponse } from '@/lib/api/errors';
import type { ApiResponse } from '@/lib/api/types';
import type { AccessFilter } from '@/features/proponent-overview/api/types';
import type {
  Checklist,
  Contact,
  Declaration,
  Member,
  MemberLevel,
  DocFile,
  DocumentOptions,
  FileRequest,
  FundingSection,
  UploadGrant,
  InstitutionSection,
  Profile,
  Project,
  ProjectStage,
  ProjectStatus,
  ProjectSummary,
  ProponentSection,
  ProposalInput,
  FilePreview,
  ReviewDecision,
  ReviewKind,
  StageHistory
} from './types';

const json = (body: unknown): RequestInit => ({ body: JSON.stringify(body) });

export async function getProfile() {
  return (await apiClient<ApiResponse<Profile>>('/admin/proponent-profile')).data;
}

export async function saveProfile(input: Contact) {
  return (
    await apiClient<ApiResponse<Profile>>('/admin/proponent-profile', {
      method: 'PUT',
      ...json(input)
    })
  ).data;
}

export interface ListProjectsParams {
  q?: string;
  page?: number;
  pageSize?: number;
  status?: ProjectStatus;
  /** Só o que é da própria pessoa: titular (`owner`), membro (`member`) ou um dos dois (`all`). Sem o parâmetro vale só a permissão. */
  access?: AccessFilter;
  /** Filtros das filas da equipe. */
  stage?: ProjectStage[];
  framing?: 'pending' | 'confirmed' | 'none';
  department?: string;
  analyst?: string;
  /** Só projetos enviados na etapa atual há pelo menos tantos dias. */
  idleDays?: number;
  /** Ex.: 'stage_since:asc' (há mais tempo na etapa primeiro). */
  sort?: string;
}

export async function listProjects({
  q,
  page = 1,
  pageSize = 25,
  status,
  access,
  stage,
  framing,
  department,
  analyst,
  idleDays,
  sort
}: ListProjectsParams = {}) {
  const params = new URLSearchParams({ page: String(page), pageSize: String(pageSize) });
  if (q) params.set('q', q);
  if (status) params.set('status', status);
  if (access) params.set('access', access);
  if (stage?.length) params.set('stage', stage.join(','));
  if (framing) params.set('framing', framing);
  if (department) params.set('department', department);
  if (analyst) params.set('analyst', analyst);
  if (idleDays) params.set('idleDays', String(idleDays));
  if (sort) params.set('sort', sort);
  return await apiClient<ApiResponse<ProjectSummary[]>>(`/admin/projects?${params}`);
}

export async function getProject(id: string) {
  return (await apiClient<ApiResponse<Project>>(`/admin/projects/${id}`)).data;
}

export async function createProject() {
  return (await apiClient<ApiResponse<Project>>('/admin/projects', { method: 'POST' })).data;
}

export async function deleteProject(id: string) {
  await apiClient<void>(`/admin/projects/${id}`, { method: 'DELETE' });
}

type WithVersion<T> = T & { version: number };

async function saveSection<T>(id: string, section: string, body: WithVersion<T>) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/${section}`, {
      method: 'PUT',
      ...json(body)
    })
  ).data;
}

export const saveProponent = (
  id: string,
  body: WithVersion<Omit<ProponentSection, 'name' | 'cpf'>>
) => saveSection(id, 'proponent', body);

export const saveInstitution = (
  id: string,
  body: WithVersion<Omit<InstitutionSection, 'entityTypeIsOther' | 'entityTypeCode'>>
) => saveSection(id, 'institution', body);

export const saveFunding = (
  id: string,
  body: WithVersion<Omit<FundingSection, 'amendmentTotalCents' | 'totalCents'>>
) => saveSection(id, 'funding', body);

export const saveProposal = (id: string, body: WithVersion<ProposalInput>) =>
  saveSection(id, 'proposal', body);

export const saveDocumentOptions = (id: string, body: WithVersion<DocumentOptions>) =>
  saveSection(id, 'document-options', body);

const docs = (id: string) => `/admin/projects/${id}/documents`;

export async function getChecklist(id: string) {
  return (await apiClient<ApiResponse<Checklist>>(docs(id))).data;
}

export async function getStageHistory(id: string) {
  return (await apiClient<ApiResponse<StageHistory>>(`/admin/projects/${id}/history`)).data;
}

/** Passo 1 do envio: registra o arquivo e obtém a autorização para enviá-lo ao armazenamento. */
export async function requestFileUpload(id: string, code: string, input: FileRequest) {
  return (
    await apiClient<ApiResponse<{ file: DocFile; upload: UploadGrant }>>(
      `${docs(id)}/${code}/files`,
      {
        method: 'POST',
        ...json(input)
      }
    )
  ).data;
}

/** Passo 3: a API confere o objeto no armazenamento e confirma o arquivo. */
export async function confirmFileUpload(id: string, fileId: string) {
  return (
    await apiClient<ApiResponse<DocFile>>(`${docs(id)}/files/${fileId}/confirm`, { method: 'POST' })
  ).data;
}

export async function getFileDownload(id: string, fileId: string) {
  return (
    await apiClient<ApiResponse<{ url: string; expiresAt: string }>>(
      `${docs(id)}/files/${fileId}/download`
    )
  ).data;
}

/** URL para exibir o arquivo no navegador (o Word vem convertido em PDF). Não conta como ter visto: use `markFileViewed`. */
export async function getFilePreview(id: string, fileId: string) {
  return (await apiClient<ApiResponse<FilePreview>>(`${docs(id)}/files/${fileId}/preview`)).data;
}

/** Registra que o arquivo foi exibido para quem consulta: é a prova exigida para aprovar ou reprovar o documento. */
export async function markFileViewed(id: string, fileId: string) {
  await apiClient<void>(`${docs(id)}/files/${fileId}/viewed`, { method: 'POST' });
}

/** Registra que quem consulta rolou até o fim do arquivo: é a prova exigida para aprovar o documento. */
export async function markFileRead(id: string, fileId: string) {
  await apiClient<void>(`${docs(id)}/files/${fileId}/read`, { method: 'POST' });
}

export async function deleteProjectFile(id: string, fileId: string) {
  await apiClient<void>(`${docs(id)}/files/${fileId}`, { method: 'DELETE' });
}

/** Cria o documento adicional (sem arquivo ainda); devolve o checklist atualizado. */
export async function createExtraDocument(
  id: string,
  input: { title: string; description: string }
) {
  return (
    await apiClient<ApiResponse<Checklist>>(`${docs(id)}/extra`, { method: 'POST', ...json(input) })
  ).data;
}

export async function updateExtraDocument(
  id: string,
  itemId: string,
  input: { title: string; description: string }
) {
  return (
    await apiClient<ApiResponse<Checklist>>(`${docs(id)}/extra/${itemId}`, {
      method: 'PUT',
      ...json(input)
    })
  ).data;
}

/** Remove o documento adicional e o arquivo dele. */
export async function deleteExtraDocument(id: string, itemId: string) {
  return (
    await apiClient<ApiResponse<Checklist>>(`${docs(id)}/extra/${itemId}`, { method: 'DELETE' })
  ).data;
}

/** Passo 1 do envio do arquivo de um documento adicional. */
export async function requestExtraFileUpload(id: string, itemId: string, input: FileRequest) {
  return (
    await apiClient<ApiResponse<{ file: DocFile; upload: UploadGrant }>>(
      `${docs(id)}/extra/${itemId}/files`,
      { method: 'POST', ...json(input) }
    )
  ).data;
}

export async function saveJustification(
  id: string,
  code: string,
  input: { amendmentId?: string; text: string }
) {
  return (
    await apiClient<ApiResponse<Checklist>>(`${docs(id)}/${code}/justification`, {
      method: 'PUT',
      ...json(input)
    })
  ).data;
}

export async function submitProject(id: string, input: { version: number; declaration: boolean }) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/submit`, {
      method: 'POST',
      ...json(input)
    })
  ).data;
}

/** Comprovante de recebimento (PDF). A resposta não é JSON, por isso não passa pelo apiClient. */
export async function downloadReceipt(id: string): Promise<Blob> {
  const res = await fetch(`/bff/admin/projects/${id}/receipt`);
  if (!res.ok) throw await apiErrorFromResponse(res);
  return res.blob();
}

export async function resubmitProject(
  id: string,
  input: { version: number; declaration: boolean }
) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/resubmit`, {
      method: 'POST',
      ...json(input)
    })
  ).data;
}

export async function withdrawProject(id: string, note: string) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/withdraw`, {
      method: 'POST',
      ...json({ note })
    })
  ).data;
}

export async function duplicateProject(id: string, copyDocuments: boolean) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/duplicate`, {
      method: 'POST',
      ...json({ copyDocuments })
    })
  ).data;
}

export interface MemberInput {
  level: MemberLevel;
  label: string;
  /** Opcional: avisa quem ainda não tem conta (quem já tem é avisado no e-mail da conta). */
  email?: string;
}

export async function listMembers(id: string) {
  return (await apiClient<ApiResponse<Member[]>>(`/admin/projects/${id}/members`)).data;
}

export async function inviteMember(id: string, input: MemberInput & { cpf: string }) {
  return (
    await apiClient<ApiResponse<Member>>(`/admin/projects/${id}/members`, {
      method: 'POST',
      ...json(input)
    })
  ).data;
}

export async function updateMember(id: string, memberId: string, input: MemberInput) {
  return (
    await apiClient<ApiResponse<Member>>(`/admin/projects/${id}/members/${memberId}`, {
      method: 'PUT',
      ...json(input)
    })
  ).data;
}

export async function revokeMember(id: string, memberId: string) {
  await apiClient<void>(`/admin/projects/${id}/members/${memberId}`, { method: 'DELETE' });
}

const declaration = (id: string, code: string) => `/admin/projects/${id}/declarations/${code}`;

/** Prévia da declaração e situação da última emissão (a consulta também conclui a assinatura já feita no MT-ID). */
export async function getDeclaration(id: string, code: string) {
  return (await apiClient<ApiResponse<Declaration>>(declaration(id, code))).data;
}

/** Aceita o texto lido, gera o PDF e pede a assinatura ao MT-ID. */
export async function acceptDeclaration(id: string, code: string, paramsSha256: string) {
  return (
    await apiClient<ApiResponse<Declaration>>(`${declaration(id, code)}/accept`, {
      method: 'POST',
      ...json({ paramsSha256, confirm: true })
    })
  ).data;
}

export async function cancelDeclaration(id: string, code: string) {
  await apiClient<void>(declaration(id, code), { method: 'DELETE' });
}

// ---- Equipe: conferência, enquadramento e decisões ----

const post = async (id: string, path: string, body?: unknown) =>
  (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/${path}`, {
      method: 'POST',
      ...(body === undefined ? {} : json(body))
    })
  ).data;

/** O Analista assume a análise ("Aguardando análise de checklist" passa a "Em análise"). */
export const startAnalysis = (id: string) => post(id, 'start-analysis');

/** O Analista assume ativamente o projeto que está com outro analista: passa a ser o responsável pela análise. */
export const takeOverAnalysis = (id: string) => post(id, 'take-over');

/**
 * Aprova, reprova (com justificativa) ou desfaz a decisão sobre um item. Decidir sobre documento com arquivo exige ter visto cada
 * arquivo no visualizador.
 */
export async function reviewItem(
  id: string,
  input: { kind: ReviewKind; target: string; decision: ReviewDecision; note?: string }
) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/review/items`, {
      method: 'PUT',
      ...json(input)
    })
  ).data;
}

/** Devolve itens (seções, documentos do checklist e documentos adicionais) ao proponente, com prazo. */
export const requestComplement = (
  id: string,
  input: {
    message: string;
    items: { kind: 'section' | 'document' | 'extra'; target: string; note: string }[];
  }
) => post(id, 'complement-requests', input);

/** Arquivamento pelo Analista por falta de documentos obrigatórios. */
export const archiveProject = (id: string, note: string) =>
  post(id, 'archive', { reason: 'missing_documents', note });

/** Negação pelo Secretário de Estado: arquiva na hora. */
export const denyProject = (id: string, note: string) => post(id, 'deny', { note });

/** Indica o departamento de enquadramento (exige tudo aprovado). */
export async function assignDepartment(id: string, departmentId: string) {
  return (
    await apiClient<ApiResponse<Project>>(`/admin/projects/${id}/department`, {
      method: 'PUT',
      ...json({ departmentId })
    })
  ).data;
}

/** O Revisor do Departamento confirma o enquadramento; `round` é a rodada que ele viu. */
export const confirmFraming = (id: string, round: number) => post(id, 'framing/confirm', { round });

/** O Revisor recusa o enquadramento, com justificativa; o projeto volta ao Analista. */
export const refuseFraming = (id: string, round: number, justification: string) =>
  post(id, 'framing/refuse', { round, justification });
