import {
  fundingTypeLabels,
  instrumentLabels,
  objectNatureLabels,
  type Project
} from '../../api/types';
import { formatBrl, formatCnpj } from '../../format';
import { AmendmentRows, ExecutionRows, documentsLine } from './parts';
import { ViewCard, ViewList, ViewRow } from './primitives';

/**
 * Resumo enxuto do projeto, para quem só precisa decidir (o Revisor do Departamento): identificação, proposta e recursos. Do proponente,
 * só o nome e o cargo; contatos e dados pessoais ficam na visualização completa. Documentos aparecem só em contagem.
 */
export function ProjectSummary({ project }: { project: Project }) {
  const { proponent: pr, institution: i, proposal: p, funding: f, documents: d } = project;
  return (
    <div className='max-w-3xl space-y-4'>
      <ViewCard title='Identificação'>
        <ViewList>
          <ViewRow label='Instrumento'>
            {i.instrument ? instrumentLabels[i.instrument] : ''}
          </ViewRow>
          <ViewRow label='Título' showEmpty>
            {p.title}
          </ViewRow>
          <ViewRow label='Nº no SIGCon'>{p.sigconNumber}</ViewRow>
          <ViewRow label='Instituição' showEmpty>
            {i.name}
            {i.cnpj && <span className='text-muted-foreground'> · {formatCnpj(i.cnpj)}</span>}
          </ViewRow>
          <ViewRow label='Proponente'>
            {pr.name}
            {pr.jobTitle && <span className='text-muted-foreground'> · {pr.jobTitle}</span>}
          </ViewRow>
        </ViewList>
      </ViewCard>

      <ViewCard title='Proposta'>
        <ViewList>
          <ViewRow label='Objeto' showEmpty>
            {p.object}
          </ViewRow>
          <ViewRow label='Natureza do objeto'>
            {p.objectNature ? objectNatureLabels[p.objectNature] : ''}
          </ViewRow>
          <ViewRow label='Justificativa' showEmpty>
            {p.justification}
          </ViewRow>
          <ExecutionRows proposal={p} />
        </ViewList>
      </ViewCard>

      <ViewCard title='Recursos'>
        <ViewList>
          <ViewRow label='Tipo de recurso'>{f.type ? fundingTypeLabels[f.type] : ''}</ViewRow>
          <AmendmentRows funding={f} />
          <ViewRow label='Total solicitado'>{f.totalCents ? formatBrl(f.totalCents) : ''}</ViewRow>
          <ViewRow label='Contrapartida'>
            {f.counterpartCents ? formatBrl(f.counterpartCents) : ''}
          </ViewRow>
          <ViewRow label='Documentos'>{documentsLine(d)}</ViewRow>
        </ViewList>
      </ViewCard>
    </div>
  );
}
