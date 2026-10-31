/** Ações do backend usadas para esconder elementos de UI (a autorização real é do backend). */
export const projectActions = {
  read: 'admin::projects.read',
  create: 'admin::projects.create',
  update: 'admin::projects.update',
  delete: 'admin::projects.delete',
  submit: 'admin::projects.submit',
  withdraw: 'admin::projects.withdraw',
  share: 'admin::projects.share',
  // Equipe: o Analista confere e indica o departamento; o Revisor decide o enquadramento; o Secretário nega.
  requestComplement: 'admin::projects.request-complement',
  archive: 'admin::projects.archive',
  deny: 'admin::projects.deny',
  startAnalysis: 'admin::projects.start-analysis',
  reviewItem: 'admin::projects.review-item',
  assignDepartment: 'admin::projects.assign-department',
  reviewFraming: 'admin::projects.review-framing',
  profileRead: 'admin::proponent-profile.read',
  profileUpdate: 'admin::proponent-profile.update'
} as const;

export type Instrument = 'convenio' | 'fomento';
export type ProjectStatus = 'draft' | 'submitted' | 'archived';
export type ProjectStage =
  | 'awaiting_checklist'
  | 'in_analysis'
  | 'awaiting_proponent'
  | 'awaiting_opinion';
export type PhoneType = 'mobile' | 'landline';
export type MemberLevel = 'read' | 'edit';
/** owner = titular; edit/read = convidado por CPF; vazio = equipe. */
export type ProjectAccess = 'owner' | MemberLevel | '';

export interface Phone {
  number: string;
  type: PhoneType;
}

export interface Email {
  address: string;
  primary: boolean;
}

export type MaritalStatus =
  | 'single'
  | 'married'
  | 'stable_union'
  | 'separated'
  | 'divorced'
  | 'widowed';

/** Dados pessoais de quem assina as declarações. O CPF identifica a pessoa: não se pede RG. */
export interface PersonalData {
  nationality: string;
  maritalStatus: MaritalStatus | '';
  zip: string;
  street: string;
  number: string;
  complement: string;
  district: string;
  uf: string;
  municipalityIbge: string;
}

/** Dados de contato do proponente, comuns ao perfil e ao projeto. */
export interface Contact {
  phones: Phone[];
  emails: Email[];
  uf: string;
  municipalityIbge: string;
  jobTitle: string;
  /** Usados nas declarações de gratuidade e de publicidade, quando o proponente é quem assina. */
  personal: PersonalData;
}

export interface Profile extends Contact {
  name: string;
  cpf: string;
  complete: boolean;
}

export interface ProponentSection extends Contact {
  name: string;
  cpf: string;
  isLegalRepresentative: boolean | null;
  legalRepName: string;
  legalRepCpf: string;
  legalRepJobTitle: string;
  /** Dados pessoais do representante legal, quando é outra pessoa. */
  legalRepPersonal: PersonalData;
}

export interface InstitutionSection {
  instrument: Instrument | '';
  cnpj: string;
  name: string;
  entityTypeId: string;
  /** Somente leitura: código do tipo escolhido; o tipo "Outro" pede o texto livre e a natureza. */
  entityTypeCode: string;
  entityTypeIsOther: boolean;
  entityTypeOther: string;
  nature: 'public' | 'private' | '';
  nonProfit: boolean;
  uf: string;
  municipalityIbge: string;
  zip: string;
  street: string;
  number: string;
  complement: string;
  district: string;
  email: string;
  phone: string;
  habilitationStatus: 'full' | 'partial' | '';
  habilitationNumber: string;
  habilitationValidUntil: string;
}

export interface ProjectMunicipality {
  ibgeCode: string;
  name: string;
  uf: string;
}

export type ObjectNature = 'works' | 'goods' | 'services' | 'event' | 'other';

export interface ProposalSection {
  sigconNumber: string;
  sigconRegisteredOn: string;
  title: string;
  object: string;
  justification: string;
  startDate: string;
  endDate: string;
  objectNature: ObjectNature | '';
  hasArtisticHiring: boolean;
  calamity: boolean;
  executionPlaceDetail: string;
  municipalities: ProjectMunicipality[];
}

/** Corpo de gravação da proposta: os municípios vão como lista de códigos IBGE. */
export type ProposalInput = Omit<ProposalSection, 'municipalities'> & {
  municipalityCodes: string[];
};

export type FundingType = 'amendment' | 'own' | 'mixed';
export type CounterpartKind = 'financial' | 'in_kind';
export type TenderStatus = 'held' | 'waived' | 'unfeasible' | 'not_applicable';

/** Emenda parlamentar. Informe `parliamentarianId` (cadastro) ou, se o parlamentar não estiver nele, o nome por extenso. */
export interface Amendment {
  /** Estável: reenviar o id mantém a emenda e os documentos anexados a ela. Vazio nas emendas novas. */
  id: string;
  parliamentarianId: string;
  parliamentarianName: string;
  number: string;
  year: number;
  amountCents: number;
}

/** Seção Recursos. Todos os valores são em centavos; os totais são calculados pelo servidor. */
export interface FundingSection {
  type: FundingType | '';
  ownAmountCents: number;
  amendments: Amendment[];
  amendmentTotalCents: number;
  totalCents: number;
  counterpartKind: CounterpartKind | '';
  counterpartCents: number;
  counterpartDescription: string;
  indirectCostsCents: number;
  tenderStatus: TenderStatus | '';
  tenderNoticeNumber: string;
  tenderApprovedOn: string;
  tenderLegalBasis: string;
  tenderJustification: string;
  artistFeesCents: number;
  regionalArtistFeesCents: number;
  governorWaiver: boolean;
  priceOldestCollectedOn: string;
}

/** Situações que acrescentam documentos ao checklist. */
export interface DocumentOptions {
  exclusiveSupplier: boolean;
  publicSpaceUse: boolean;
  reducedDeadline: boolean;
}

export interface DocumentSummary {
  total: number;
  done: number;
  justified: number;
  missing: number;
  /** Só os obrigatórios: é o que bloqueia a continuidade e o envio. */
  requiredTotal: number;
  requiredMissing: number;
}

export interface DocFile {
  id: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  status: 'pending' | 'confirmed';
  /** SHA-256 do conteúdo, calculado pelo servidor na confirmação. */
  sha256?: string;
  createdAt: string;
}

export interface ChecklistItem {
  code: string;
  name: string;
  description: string;
  group: string;
  scope: 'project' | 'amendment';
  amendmentId: string;
  amendmentLabel: string;
  /** Item do checklist da SEDEC (ex.: C2, F2). */
  source: string;
  template: { name: string; url: string } | null;
  acceptsJustification: boolean;
  /** Obrigatório: sem arquivo nem justificativa, bloqueia a continuidade e o envio. Falso = opcional. */
  mandatory: boolean;
  files: DocFile[];
  justification: string;
  status: 'missing' | 'done' | 'justified';
  /** A declaração assinada deste item ficou desatualizada porque o cadastro mudou: emita de novo. */
  declarationStale: boolean;
  /** A declaração foi emitida e aguarda a assinatura: nada a fazer pelo proponente. */
  declarationAwaiting: boolean;
}

/** Documento adicional do proponente: título, descrição opcional e um único arquivo. Não conta nos obrigatórios. */
export interface ExtraDocument {
  id: string;
  title: string;
  description: string;
  file: DocFile | null;
  /** O arquivo já fez parte de uma versão enviada: não pode mais ser alterado nem removido. */
  locked: boolean;
}

/** Máximo de documentos adicionais por projeto (o servidor é a autoridade). */
export const MAX_EXTRA_DOCUMENTS = 5;

export interface Checklist {
  options: DocumentOptions;
  instrument: Instrument | '';
  summary: DocumentSummary;
  items: ChecklistItem[];
  extraDocuments: ExtraDocument[];
}

export interface FileRequest {
  filename: string;
  contentType: string;
  sizeBytes: number;
  amendmentId?: string;
}

export interface UploadGrant {
  url: string;
  fields: Record<string, string>;
  expires_at: string;
}

export type SectionKey =
  | 'proponent'
  | 'institution'
  | 'proposal'
  | 'funding'
  | 'documents'
  | 'submission';

export interface Issue {
  section: SectionKey;
  field: string;
  code: string;
  severity: 'block' | 'warn';
  message: string;
}

export interface Project {
  id: string;
  status: ProjectStatus;
  version: number;
  isOwner: boolean;
  /** Relação de quem consulta com o projeto; vazio para a equipe. */
  access: ProjectAccess;
  /** Código, etapa e data só existem depois do envio. */
  code: string;
  stage: ProjectStage | '';
  submittedAt: string | null;
  lateSubmission: boolean;
  complement: Complement | null;
  archive: ArchiveInfo | null;
  /** Só a equipe recebe: analista responsável, conferência e enquadramento (o titular e os membros não). */
  analyst?: AnalystInfo;
  review?: Review;
  /** Rodada de enquadramento em curso (pendente ou confirmada) e o histórico, da mais antiga à mais nova. */
  framing?: Framing;
  framings?: Framing[];
  originProjectId: string;
  originCode: string;
  createdAt: string;
  updatedAt: string;
  proponent: ProponentSection;
  institution: InstitutionSection;
  proposal: ProposalSection;
  funding: FundingSection;
  documentOptions: DocumentOptions;
  documents: DocumentSummary;
  issues: Issue[];
}

export interface ProjectSummary {
  id: string;
  status: ProjectStatus;
  code: string;
  submittedAt: string | null;
  stage: ProjectStage | '';
  archiveReason: ArchiveReason | '';
  complementDueAt: string | null;
  version: number;
  instrument: Instrument | '';
  title: string;
  institutionName: string;
  institutionCnpj: string;
  sigconNumber: string;
  totalCents: number;
  isOwner: boolean;
  access: ProjectAccess;
  createdAt: string;
  updatedAt: string;
  /** Só a equipe recebe. */
  department?: DepartmentRef;
  framingStatus?: FramingStatus;
  analyst?: AnalystInfo;
  /** Há quanto tempo (horas corridas) o projeto enviado está na etapa atual. */
  stageSeconds?: number;
}

export const instrumentLabels: Record<Instrument, string> = {
  convenio: 'Convênio',
  fomento: 'Termo de Fomento'
};

export const statusLabels: Record<ProjectStatus, string> = {
  draft: 'Rascunho',
  submitted: 'Enviado',
  archived: 'Arquivado'
};

export const stageLabels: Record<ProjectStage, string> = {
  awaiting_checklist: 'Aguardando análise de checklist',
  in_analysis: 'Em análise',
  awaiting_proponent: 'Aguardando correções do proponente',
  awaiting_opinion: 'Aguardando parecer'
};

// ---- Histórico de etapas (equipe) ----

/** Com quem o projeto estava num trecho do histórico. */
export type HistoryHolder = 'team' | 'analyst' | 'department' | 'proponent' | 'none';

export const historyHolderLabels: Record<HistoryHolder, string> = {
  team: 'Equipe (sem analista)',
  analyst: 'Analista',
  department: 'Departamento',
  proponent: 'Proponente',
  none: 'Arquivado'
};

export interface HistorySegment {
  seq: number;
  status: ProjectStatus;
  stage: ProjectStage;
  holder: HistoryHolder;
  framingStatus?: FramingStatus;
  analyst?: { id: string; name: string };
  department?: { id: string; name: string };
  enteredAt: string;
  /** Nulo no trecho atual. */
  leftAt: string | null;
  durationSeconds: number;
  enteredBy?: { type: string; name?: string };
  /** Ação que abriu o trecho, igual à do registro de auditoria (project.submit, project.resubmit…). */
  transition: string;
  auditLogId?: string;
  /** Trecho criado pela migração para um projeto que já existia: a entrada é aproximada. */
  estimated: boolean;
}

export interface StageHistory {
  segments: HistorySegment[];
  summary: {
    sinceSubmissionSeconds: number;
    currentStageSeconds: number;
    byHolder: Partial<Record<HistoryHolder, number>>;
    byStage: Partial<Record<ProjectStage, number>>;
  };
}

// ---- Conferência e enquadramento (equipe) ----

export interface AnalystInfo {
  id: string;
  name: string;
}

/** O id some se o departamento foi excluído; o nome é o da data da indicação. */
export interface DepartmentRef {
  id: string | null;
  name: string;
}

/** Tipo do item conferido: seção, documento do checklist ou documento adicional. */
export type ReviewKind = 'section' | 'document' | 'extra';

export interface ReviewFile {
  id: string;
  filename: string;
  /** De quem consulta: cada Analista vê os arquivos que ele mesmo viu no visualizador (a primeira página apareceu). */
  opened: boolean;
  /** Chegou ao fim do arquivo: é o que libera "Aprovar" (reprovar só pede ter visto). */
  read: boolean;
}

/** Decisão sobre um item da conferência; `pending` desfaz. */
export type ReviewDecision = 'approved' | 'rejected' | 'pending';

/** Como exibir o arquivo no navegador: PDF (inclusive o Word convertido) ou imagem. */
export interface FilePreview {
  url: string;
  expiresAt: string;
  kind: 'pdf' | 'image';
  contentType: string;
  /** O que se vê é o PDF gerado a partir do Word, não o arquivo enviado. */
  converted: boolean;
}

export interface ReviewItem {
  kind: ReviewKind;
  /** Seção, chave do documento (`codigo` ou `codigo@emenda`) ou id do documento adicional. */
  target: string;
  label: string;
  status: ReviewDecision;
  /** Justificativa da reprovação. */
  note?: string;
  /** Impedimento: documento obrigatório sem arquivo nem justificativa. Não há o que aprovar. */
  blocking: boolean;
  reason?: string;
  reviewedBy?: string;
  reviewedAt?: string;
  files: ReviewFile[];
}

export interface Review {
  items: ReviewItem[];
  summary: { total: number; approved: number; rejected: number; pending: number; blockers: number };
  /** Há itens, todos aprovados e nenhum reprovado nem impedimento: condição para indicar o departamento. */
  allApproved: boolean;
}

export type FramingStatus = 'pending' | 'confirmed';
export type FramingDecision = 'pending' | 'confirmed' | 'refused';

/** Uma rodada de enquadramento: o Analista indicou um departamento e o Revisor dele decidiu (ou ainda não). */
export interface Framing {
  round: number;
  department: DepartmentRef;
  decision: FramingDecision;
  indicatedBy: string;
  indicatedAt: string;
  decidedBy?: string;
  decidedAt?: string;
  justification?: string;
}

export const framingStatusLabels: Record<FramingStatus, string> = {
  pending: 'Aguardando decisão do departamento',
  confirmed: 'Enquadrado'
};

export const framingDecisionLabels: Record<FramingDecision, string> = {
  pending: 'Pendente',
  confirmed: 'Confirmado',
  refused: 'Recusado'
};

export const reviewKindLabels: Record<ReviewKind, string> = {
  section: 'Seções do cadastro',
  document: 'Documentos',
  extra: 'Documentos adicionais'
};

export type ArchiveReason =
  | 'complement_deadline'
  | 'withdrawn'
  | 'denied'
  | 'missing_documents'
  | 'other';

export const archiveReasonLabels: Record<ArchiveReason, string> = {
  complement_deadline: 'Prazo de correção vencido',
  withdrawn: 'Desistência do proponente',
  denied: 'Negado pela Secretaria',
  missing_documents: 'Falta de documentos obrigatórios',
  other: 'Decisão da equipe'
};

/** Item devolvido pela equipe: uma seção, um documento do checklist ou um documento adicional. */
export interface ComplementItem {
  kind: 'section' | 'document' | 'extra';
  /** Seção: proponent, institution, proposal, funding ou document-options. Documento: código, ou código@id-da-emenda. Adicional: id. */
  target: string;
  label: string;
  note: string;
  answered: boolean;
}

export interface Complement {
  number: number;
  status: 'open' | 'answered' | 'expired' | 'cancelled';
  requestedAt: string;
  dueAt: string;
  overdue: boolean;
  message: string;
  items: ComplementItem[];
}

export interface ArchiveInfo {
  at: string;
  reason: ArchiveReason;
  note: string;
}

export const objectNatureLabels: Record<ObjectNature, string> = {
  works: 'Obra ou serviço de engenharia',
  goods: 'Aquisição de bens',
  services: 'Contratação de serviços',
  event: 'Evento',
  other: 'Outro'
};

export const sectionLabels: Record<SectionKey, string> = {
  proponent: 'Proponente',
  institution: 'Instituição',
  proposal: 'Proposta',
  funding: 'Recursos',
  documents: 'Documentos',
  submission: 'Revisão e envio'
};

export const fundingTypeLabels: Record<FundingType, string> = {
  amendment: 'Emenda parlamentar',
  own: 'Recurso próprio da SEDEC',
  mixed: 'Emenda parlamentar e recurso próprio da SEDEC'
};

/** Pessoa com acesso ao projeto. O CPF vem mascarado e a API não informa se a pessoa tem conta. */
export interface Member {
  id: string;
  cpf: string;
  label: string;
  level: MemberLevel;
  /** E-mail que o titular informou (mascarado); vazio se não informou. */
  email: string;
  createdAt: string;
  /** Primeira vez em que a pessoa abriu o projeto; vazio = convite pendente. */
  acceptedAt: string | null;
}

export const memberLevelLabels: Record<MemberLevel, string> = {
  read: 'Leitura',
  edit: 'Edição'
};

export const accessLabels: Record<Exclude<ProjectAccess, ''>, string> = {
  owner: 'Titular',
  edit: 'Edição',
  read: 'Leitura'
};

/** Documentos que o sistema gera a partir do cadastro e o representante legal assina no MT-ID. */
export const GENERATED_DECLARATIONS: readonly string[] = [
  'declaracao_contrapartida',
  'declaracao_nao_duplicidade',
  'declaracao_gratuidade',
  'declaracao_publicidade',
  'declaracao_compatibilidade_precos'
];

export type DeclarationStatus =
  | 'awaiting_signature'
  | 'signed'
  | 'rejected'
  | 'expired'
  | 'cancelled'
  | 'superseded';

/** Situação da última emissão da declaração. */
export interface DeclarationRecord {
  id: string;
  status: DeclarationStatus;
  acceptedAt: string;
  /** Falso quando quem pediu não é o signatário: o pedido vai para o signatário, que confirma no MTCidadão. */
  requestedBySigner: boolean;
  /** CPF do signatário, mascarado. */
  signerCpf: string;
  signedAt: string | null;
  fileId: string | null;
  /** O cadastro mudou depois da emissão: a declaração deixou de valer. */
  stale: boolean;
}

export interface DeclarationIssue {
  section: 'declaration';
  field: string;
  code: string;
  severity: 'block' | 'warn';
  message: string;
}

/** Prévia da declaração: o texto como será emitido e as pendências de cadastro. */
export interface Declaration {
  code: string;
  name: string;
  annex: string;
  title: string;
  paragraphs: string[];
  place: string;
  signerName: string;
  signerRole: string;
  institution: string;
  /** Resumo do conteúdo; volta no aceite para provar que esta é a versão que a pessoa leu. */
  paramsSha256: string;
  ready: boolean;
  issues: DeclarationIssue[];
  current: DeclarationRecord | null;
}
