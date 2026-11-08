'use client';

import { useQuery } from '@tanstack/react-query';
import { Button } from '@/components/ui/button';
import { entityTypesQueryOptions } from '@/features/entity-types/api/queries';
import { municipalitiesByUfQueryOptions } from '@/features/municipalities/api/queries';
import { tenderBasisOptions, tenderStatusOptions } from '../../funding-rules';
import {
  fundingTypeLabels,
  instrumentLabels,
  objectNatureLabels,
  type Contact,
  type MaritalStatus,
  type PersonalData,
  type Project
} from '../../api/types';
import { formatBrl, formatCnpj, formatCpf, formatDate, formatPhone, formatZip } from '../../format';
import { AmendmentRows, ExecutionRows } from './parts';
import { ProjectDocuments } from './project-documents';
import { ViewCard, ViewList, ViewRow } from './primitives';

/** As seções da visualização completa, na ordem da página; `id` é a âncora (`secao-<id>`). */
export const fullSections = [
  { id: 'proponente', label: 'Proponente' },
  { id: 'instituicao', label: 'Instituição' },
  { id: 'proposta', label: 'Proposta' },
  { id: 'recursos', label: 'Recursos' },
  { id: 'documentos', label: 'Documentos' }
] as const;

export type FullSectionId = (typeof fullSections)[number]['id'];

export const sectionAnchor = (id: string) => `secao-${id}`;

const maritalLabels: Record<MaritalStatus, string> = {
  single: 'Solteiro(a)',
  married: 'Casado(a)',
  stable_union: 'Em união estável',
  separated: 'Separado(a)',
  divorced: 'Divorciado(a)',
  widowed: 'Viúvo(a)'
};

const natureLabels = { public: 'Pública', private: 'Privada' } as const;
const habilitationLabels = { full: 'Plena', partial: 'Parcial' } as const;
const counterpartLabels = { financial: 'Financeira (dinheiro)', in_kind: 'Em bens e serviços' };
const yesNo = (v: boolean) => (v ? 'Sim' : 'Não');

/** Nome do município pelo código do IBGE (a lista da UF fica em cache); sem o nome, mostra o código. */
function MunicipalityName({ uf, ibge }: { uf: string; ibge: string }) {
  const list = useQuery(municipalitiesByUfQueryOptions(uf));
  if (!ibge) return null;
  return <>{list.data?.find((m) => m.ibgeCode === ibge)?.name ?? ibge}</>;
}

function Place({ uf, ibge }: { uf: string; ibge: string }) {
  if (!uf && !ibge) return null;
  return (
    <>
      <MunicipalityName uf={uf} ibge={ibge} />
      {uf && ` / ${uf}`}
    </>
  );
}

function ContactRows({ c }: { c: Contact }) {
  return (
    <>
      <ViewRow label='Cargo'>{c.jobTitle}</ViewRow>
      <ViewRow label='Telefones'>
        {c.phones.length > 0 && (
          <ul>
            {c.phones.map((p) => (
              <li key={p.number}>
                {formatPhone(p.number)}
                <span className='text-muted-foreground'>
                  {' · '}
                  {p.type === 'mobile' ? 'Celular' : 'Fixo'}
                </span>
              </li>
            ))}
          </ul>
        )}
      </ViewRow>
      <ViewRow label='E-mails'>
        {c.emails.length > 0 && (
          <ul>
            {c.emails.map((e) => (
              <li key={e.address}>
                {e.address}
                {e.primary && <span className='text-muted-foreground'> · Principal</span>}
              </li>
            ))}
          </ul>
        )}
      </ViewRow>
      <ViewRow label='Localidade'>
        {(c.uf || c.municipalityIbge) && <Place uf={c.uf} ibge={c.municipalityIbge} />}
      </ViewRow>
    </>
  );
}

/** Dados de quem assina as declarações (nacionalidade, estado civil e endereço residencial). */
function PersonalRows({ d }: { d: PersonalData }) {
  const street = [d.street, d.number, d.complement, d.district].filter(Boolean).join(', ');
  return (
    <>
      <ViewRow label='Nacionalidade'>{d.nationality}</ViewRow>
      <ViewRow label='Estado civil'>
        {d.maritalStatus ? maritalLabels[d.maritalStatus] : ''}
      </ViewRow>
      <ViewRow label='Endereço'>
        {street}
        {d.zip && <span className='text-muted-foreground'> · CEP {formatZip(d.zip)}</span>}
        {d.uf && (
          <div>
            <Place uf={d.uf} ibge={d.municipalityIbge} />
          </div>
        )}
      </ViewRow>
    </>
  );
}

function ProponentSection({ project }: { project: Project }) {
  const p = project.proponent;
  return (
    <ViewCard id={sectionAnchor('proponente')} title='Proponente'>
      <ViewList>
        <ViewRow label='Nome' showEmpty>
          {p.name}
        </ViewRow>
        <ViewRow label='CPF'>{p.cpf ? formatCpf(p.cpf) : ''}</ViewRow>
        <ContactRows c={p} />
        <ViewRow label='Representante legal' showEmpty>
          {p.isLegalRepresentative === null
            ? ''
            : p.isLegalRepresentative
              ? 'O próprio proponente'
              : 'Outra pessoa'}
        </ViewRow>
        {p.isLegalRepresentative === false && (
          <>
            <ViewRow label='Nome do representante'>{p.legalRepName}</ViewRow>
            <ViewRow label='CPF do representante'>
              {p.legalRepCpf ? formatCpf(p.legalRepCpf) : ''}
            </ViewRow>
            <ViewRow label='Cargo do representante'>{p.legalRepJobTitle}</ViewRow>
          </>
        )}
      </ViewList>
      <h4 className='mt-5 mb-2 text-sm font-medium'>Dados pessoais do proponente</h4>
      <ViewList>
        <PersonalRows d={p.personal} />
      </ViewList>
      {p.isLegalRepresentative === false && (
        <>
          <h4 className='mt-5 mb-2 text-sm font-medium'>Dados pessoais do representante legal</h4>
          <ViewList>
            <PersonalRows d={p.legalRepPersonal} />
          </ViewList>
        </>
      )}
    </ViewCard>
  );
}

function InstitutionSection({ project }: { project: Project }) {
  const i = project.institution;
  const types = useQuery(entityTypesQueryOptions());
  const typeName = i.entityTypeIsOther
    ? i.entityTypeOther
    : (types.data?.find((t) => t.id === i.entityTypeId)?.name ?? i.entityTypeCode);
  const street = [i.street, i.number, i.complement, i.district].filter(Boolean).join(', ');
  return (
    <ViewCard id={sectionAnchor('instituicao')} title='Instituição'>
      <ViewList>
        <ViewRow label='Instrumento'>{i.instrument ? instrumentLabels[i.instrument] : ''}</ViewRow>
        <ViewRow label='CNPJ'>{i.cnpj ? formatCnpj(i.cnpj) : ''}</ViewRow>
        <ViewRow label='Nome' showEmpty>
          {i.name}
        </ViewRow>
        <ViewRow label='Tipo de entidade'>{typeName}</ViewRow>
        <ViewRow label='Natureza'>{i.nature ? natureLabels[i.nature] : ''}</ViewRow>
        {i.nature === 'private' && (
          <ViewRow label='Sem fins lucrativos'>{yesNo(i.nonProfit)}</ViewRow>
        )}
        <ViewRow label='Endereço'>
          {street}
          {i.zip && <span className='text-muted-foreground'> · CEP {formatZip(i.zip)}</span>}
          {i.uf && (
            <div>
              <Place uf={i.uf} ibge={i.municipalityIbge} />
            </div>
          )}
        </ViewRow>
        <ViewRow label='E-mail'>{i.email}</ViewRow>
        <ViewRow label='Telefone'>{i.phone ? formatPhone(i.phone) : ''}</ViewRow>
        <ViewRow label='Habilitação no SIGCon'>
          {i.habilitationStatus ? habilitationLabels[i.habilitationStatus] : ''}
        </ViewRow>
        <ViewRow label='Nº da certidão'>{i.habilitationNumber}</ViewRow>
        <ViewRow label='Certidão válida até'>
          {i.habilitationValidUntil ? formatDate(i.habilitationValidUntil) : ''}
        </ViewRow>
      </ViewList>
    </ViewCard>
  );
}

function ProposalSection({ project }: { project: Project }) {
  const p = project.proposal;
  return (
    <ViewCard id={sectionAnchor('proposta')} title='Proposta'>
      <ViewList>
        <ViewRow label='Nº no SIGCon'>{p.sigconNumber}</ViewRow>
        <ViewRow label='Cadastro no SIGCon'>
          {p.sigconRegisteredOn ? formatDate(p.sigconRegisteredOn) : ''}
        </ViewRow>
        <ViewRow label='Título' showEmpty>
          {p.title}
        </ViewRow>
        <ViewRow label='Objeto' showEmpty>
          {p.object}
        </ViewRow>
        <ViewRow label='Justificativa' showEmpty>
          {p.justification}
        </ViewRow>
        <ViewRow label='Natureza do objeto'>
          {p.objectNature ? objectNatureLabels[p.objectNature] : ''}
        </ViewRow>
        <ViewRow label='Contratação de artistas'>{yesNo(p.hasArtisticHiring)}</ViewRow>
        <ViewRow label='Calamidade ou emergência'>{yesNo(p.calamity)}</ViewRow>
        <ExecutionRows proposal={p} />
      </ViewList>
    </ViewCard>
  );
}

function FundingSection({ project }: { project: Project }) {
  const f = project.funding;
  const instrument = project.institution.instrument;
  const tenderStatus = f.tenderStatus
    ? f.tenderStatus === 'not_applicable'
      ? 'Não se aplica'
      : (instrument ? tenderStatusOptions[instrument] : []).find((o) => o.value === f.tenderStatus)
          ?.label
    : '';
  const tenderBasis = tenderBasisOptions[`${instrument}:${f.tenderStatus}`]?.find(
    (o) => o.value === f.tenderLegalBasis
  )?.label;
  return (
    <ViewCard id={sectionAnchor('recursos')} title='Recursos'>
      <ViewList>
        <ViewRow label='Tipo de recurso'>{f.type ? fundingTypeLabels[f.type] : ''}</ViewRow>
        {(f.type === 'own' || f.type === 'mixed') && (
          <ViewRow label='Recurso próprio'>{formatBrl(f.ownAmountCents)}</ViewRow>
        )}
        <AmendmentRows funding={f} />
        <ViewRow label='Total solicitado'>{f.totalCents ? formatBrl(f.totalCents) : ''}</ViewRow>
        <ViewRow label='Contrapartida'>
          {f.counterpartKind
            ? `${counterpartLabels[f.counterpartKind]} · ${formatBrl(f.counterpartCents)}`
            : 'Sem contrapartida'}
        </ViewRow>
        <ViewRow label='Bens e serviços'>{f.counterpartDescription}</ViewRow>
        <ViewRow label='Custos indiretos'>
          {f.indirectCostsCents ? formatBrl(f.indirectCostsCents) : ''}
        </ViewRow>
        <ViewRow label='Referência de preço mais antiga'>
          {f.priceOldestCollectedOn ? formatDate(f.priceOldestCollectedOn) : ''}
        </ViewRow>
        <ViewRow label='Chamamento público'>{tenderStatus}</ViewRow>
        <ViewRow label='Nº do edital'>{f.tenderNoticeNumber}</ViewRow>
        <ViewRow label='Homologação'>
          {f.tenderApprovedOn ? formatDate(f.tenderApprovedOn) : ''}
        </ViewRow>
        <ViewRow label='Fundamento legal'>{tenderBasis}</ViewRow>
        <ViewRow label='Justificativa'>{f.tenderJustification}</ViewRow>
        {project.proposal.hasArtisticHiring && (
          <>
            <ViewRow label='Cachês artísticos'>{formatBrl(f.artistFeesCents)}</ViewRow>
            <ViewRow label='Cachês de regionais'>{formatBrl(f.regionalArtistFeesCents)}</ViewRow>
            <ViewRow label='Autorização do Governador'>{yesNo(f.governorWaiver)}</ViewRow>
          </>
        )}
      </ViewList>
    </ViewCard>
  );
}

/** Índice no topo: leva a cada seção sem mexer no endereço da página. */
function SectionIndex() {
  return (
    <nav aria-label='Seções do projeto' className='flex flex-wrap gap-1'>
      {fullSections.map((s) => (
        <Button
          key={s.id}
          type='button'
          size='sm'
          variant='outline'
          onClick={() =>
            document.getElementById(sectionAnchor(s.id))?.scrollIntoView({ block: 'start' })
          }
        >
          {s.label}
        </Button>
      ))}
    </nav>
  );
}

/**
 * Visualização completa do projeto, em leitura: todas as seções do cadastro, incluindo contatos, dados pessoais e a lista de
 * documentos. Não é o formulário do proponente; nada aqui é editável.
 */
export function ProjectFull({ project }: { project: Project }) {
  return (
    <div className='max-w-4xl space-y-4'>
      <SectionIndex />
      <ProponentSection project={project} />
      <InstitutionSection project={project} />
      <ProposalSection project={project} />
      <FundingSection project={project} />
      <ViewCard id={sectionAnchor('documentos')} title='Documentos'>
        <ProjectDocuments project={project} />
      </ViewCard>
    </div>
  );
}
