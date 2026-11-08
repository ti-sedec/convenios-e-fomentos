'use client';

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';
import { toast } from 'sonner';
import { useStore } from '@tanstack/react-form';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { FieldGroup } from '@/components/ui/field';
import { ReadOnlyScope } from '@/components/forms/read-only-scope';
import { useAppForm } from '@/lib/form';
import { invalidateAfterSave, projectKeys } from '../api/queries';
import { saveFunding } from '../api/service';
import {
  fundingTypeLabels,
  type CounterpartKind,
  type FundingType,
  type Project,
  type TenderStatus
} from '../api/types';
import { tenderBasisOptions, tenderRequired, tenderStatusOptions } from '../funding-rules';
import { formatBrl } from '../format';
import { AmendmentsEditor, fromRow, toRow, type AmendmentRow } from './amendments-editor';
import { MoneyField } from './money-input';
import { ServerErrors } from './server-errors';
import { StepActions } from './step-actions';

const typeOptions = Object.entries(fundingTypeLabels).map(([value, label]) => ({ value, label }));

export function FundingStep({
  project,
  readOnly,
  onSaved,
  onDirty
}: {
  project: Project;
  readOnly: boolean;
  onSaved: (project: Project, advance: boolean) => void;
  onDirty: (dirty: boolean) => void;
}) {
  const qc = useQueryClient();
  const advance = useRef(false);
  const f = project.funding;
  const { instrument, nature } = project.institution;
  const hiring = project.proposal.hasArtisticHiring;

  const defaults = () => ({
    type: f.type as string,
    ownAmountCents: f.ownAmountCents,
    amendments: f.amendments.map(toRow) as AmendmentRow[],
    counterpartKind: f.counterpartKind as string,
    counterpartCents: f.counterpartCents,
    counterpartDescription: f.counterpartDescription,
    indirectCostsCents: f.indirectCostsCents,
    tenderStatus: f.tenderStatus as string,
    tenderNoticeNumber: f.tenderNoticeNumber,
    tenderApprovedOn: f.tenderApprovedOn,
    tenderLegalBasis: f.tenderLegalBasis,
    tenderJustification: f.tenderJustification,
    artistFeesCents: f.artistFeesCents,
    regionalArtistFeesCents: f.regionalArtistFeesCents,
    governorWaiver: f.governorWaiver,
    priceOldestCollectedOn: f.priceOldestCollectedOn
  });

  const save = useMutation({
    mutationFn: (v: ReturnType<typeof defaults>) => {
      const hasAmendments = v.type === 'amendment' || v.type === 'mixed';
      const hasOwn = v.type === 'own' || v.type === 'mixed';
      const tenderOpen = v.tenderStatus === 'waived' || v.tenderStatus === 'unfeasible';
      // Campos que a tela esconde não vão ao servidor: o que não se aplica à escolha atual é zerado.
      return saveFunding(project.id, {
        version: project.version,
        type: v.type as FundingType | '',
        ownAmountCents: hasOwn ? v.ownAmountCents : 0,
        amendments: hasAmendments ? v.amendments.map(fromRow) : [],
        counterpartKind: v.counterpartKind as CounterpartKind | '',
        counterpartCents: v.counterpartKind ? v.counterpartCents : 0,
        counterpartDescription: v.counterpartKind === 'in_kind' ? v.counterpartDescription : '',
        indirectCostsCents: instrument === 'fomento' ? v.indirectCostsCents : 0,
        tenderStatus: v.tenderStatus as TenderStatus | '',
        tenderNoticeNumber: v.tenderStatus === 'held' ? v.tenderNoticeNumber : '',
        tenderApprovedOn: v.tenderStatus === 'held' ? v.tenderApprovedOn : '',
        tenderLegalBasis: tenderOpen ? v.tenderLegalBasis : '',
        tenderJustification: tenderOpen ? v.tenderJustification : '',
        artistFeesCents: hiring ? v.artistFeesCents : 0,
        regionalArtistFeesCents: hiring ? v.regionalArtistFeesCents : 0,
        governorWaiver: hiring ? v.governorWaiver : false,
        priceOldestCollectedOn: instrument === 'fomento' ? v.priceOldestCollectedOn : ''
      });
    },
    onSuccess: (saved) => {
      qc.setQueryData(projectKeys.detail(project.id), saved);
      void invalidateAfterSave(qc, project.id);
      toast.success('Recursos salvos.');
      onSaved(saved, advance.current);
    }
  });

  const form = useAppForm({
    defaultValues: defaults(),
    // Somente leitura (a equipe, ou projeto que o proponente não pode mais alterar): nada é enviado ao servidor.
    onSubmit: ({ value }) => (readOnly ? undefined : save.mutateAsync(value).catch(() => undefined))
  });
  const dirty = useStore(form.store, (s) => s.isDirty);
  useEffect(() => onDirty(dirty), [dirty, onDirty]);
  const v = useStore(form.store, (s) => s.values);

  const hasOwn = v.type === 'own' || v.type === 'mixed';
  const hasAmendments = v.type === 'amendment' || v.type === 'mixed';
  const amendmentsTotal = hasAmendments
    ? v.amendments.reduce((sum, a) => sum + a.amountCents, 0)
    : 0;
  const total = (hasOwn ? v.ownAmountCents : 0) + amendmentsTotal;
  const needsTender = tenderRequired({ instrument, nature }, v.type as FundingType | '');
  const statusOptions = instrument ? tenderStatusOptions[instrument] : [];
  const basisOptions = tenderBasisOptions[`${instrument}:${v.tenderStatus}`] ?? [];
  const counterpartOptions =
    instrument === 'fomento'
      ? [{ value: 'in_kind', label: 'Em bens e serviços' }]
      : [
          { value: 'financial', label: 'Financeira (dinheiro)' },
          { value: 'in_kind', label: 'Em bens e serviços' }
        ];

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        void form.handleSubmit();
      }}
    >
      <ReadOnlyScope readOnly={readOnly}>
        <FieldGroup>
          {!instrument && (
            <Alert>
              <AlertDescription>
                Escolha o instrumento na etapa Instituição: ele define as regras de contrapartida,
                custos indiretos e chamamento público.
              </AlertDescription>
            </Alert>
          )}

          <form.AppField
            name='type'
            children={(field) => (
              <field.RadioGroupField
                label='Tipo de recurso'
                required
                description='"Recurso próprio" é o recurso orçamentário da SEDEC; a contrapartida do proponente é informada mais abaixo.'
                options={typeOptions}
              />
            )}
          />

          {hasOwn && (
            <form.Field
              name='ownAmountCents'
              children={(field) => (
                <MoneyField
                  id='own-amount'
                  label='Valor do recurso próprio da SEDEC'
                  required
                  value={field.state.value}
                  onChange={field.handleChange}
                />
              )}
            />
          )}
          {hasAmendments && (
            <form.Field
              name='amendments'
              children={(field) => (
                <AmendmentsEditor value={field.state.value} onChange={field.handleChange} />
              )}
            />
          )}
          {v.type && (
            <dl
              className='bg-muted/40 grid gap-1 rounded-lg border p-3 text-sm sm:grid-cols-3'
              aria-label='Resumo dos valores'
            >
              {hasOwn && (
                <div>
                  <dt className='text-muted-foreground'>Recurso próprio</dt>
                  <dd className='tabular-nums'>{formatBrl(v.ownAmountCents)}</dd>
                </div>
              )}
              {hasAmendments && (
                <div>
                  <dt className='text-muted-foreground'>Emendas</dt>
                  <dd className='tabular-nums'>{formatBrl(amendmentsTotal)}</dd>
                </div>
              )}
              <div>
                <dt className='text-muted-foreground'>Total solicitado</dt>
                <dd className='font-semibold tabular-nums'>{formatBrl(total)}</dd>
              </div>
            </dl>
          )}

          <fieldset className='space-y-4 rounded-lg border p-4'>
            <legend className='px-1 text-sm font-medium'>Contrapartida do proponente</legend>
            <p className='text-muted-foreground text-sm'>
              {instrument === 'fomento'
                ? 'No Termo de Fomento a contrapartida não pode ser exigida em dinheiro: só em bens e serviços, com o valor em reais.'
                : 'Convênio: os limites seguem a LDO do Estado. Em calamidade ou emergência a contrapartida é dispensada.'}
            </p>
            <form.AppField
              name='counterpartKind'
              children={(field) => (
                <field.SelectField
                  label='Tipo'
                  placeholder='Sem contrapartida'
                  options={[{ value: '', label: 'Sem contrapartida' }, ...counterpartOptions]}
                />
              )}
            />
            {v.counterpartKind && (
              <form.Field
                name='counterpartCents'
                children={(field) => (
                  <MoneyField
                    id='counterpart'
                    label='Valor da contrapartida'
                    required
                    value={field.state.value}
                    onChange={field.handleChange}
                  />
                )}
              />
            )}
            {v.counterpartKind === 'in_kind' && (
              <form.AppField
                name='counterpartDescription'
                children={(field) => (
                  <field.TextareaField
                    label='Bens e serviços oferecidos'
                    required
                    rows={3}
                    maxLength={1000}
                    showCount
                  />
                )}
              />
            )}
          </fieldset>

          {instrument === 'fomento' && (
            <fieldset className='space-y-4 rounded-lg border p-4'>
              <legend className='px-1 text-sm font-medium'>Custos indiretos e preços</legend>
              <form.Field
                name='indirectCostsCents'
                children={(field) => (
                  <MoneyField
                    id='indirect-costs'
                    label='Custos indiretos'
                    description='Até 20% (teto de R$ 15 mil) para parcerias de até R$ 100 mil; 15% (R$ 50 mil) até R$ 500 mil; 10% (R$ 200 mil) acima disso. IN Conjunta 002/2026.'
                    value={field.state.value}
                    onChange={field.handleChange}
                  />
                )}
              />
              <form.AppField
                name='priceOldestCollectedOn'
                children={(field) => (
                  <field.TextField
                    label='Data da referência de preço mais antiga'
                    type='date'
                    description='As referências de preço devem ter no máximo 60 dias.'
                  />
                )}
              />
            </fieldset>
          )}

          {(needsTender || v.tenderStatus) && instrument && (
            <fieldset className='space-y-4 rounded-lg border p-4'>
              <legend className='px-1 text-sm font-medium'>Chamamento público</legend>
              <p className='text-muted-foreground text-sm'>
                {needsTender
                  ? 'Este projeto exige chamamento público. Informe o chamamento realizado ou o fundamento da dispensa ou inexigibilidade.'
                  : 'Este projeto não exige chamamento público.'}
              </p>
              <form.AppField
                name='tenderStatus'
                children={(field) => (
                  <field.SelectField
                    label='Situação'
                    required={needsTender}
                    placeholder='Escolha'
                    options={[{ value: '', label: 'Escolha' }, ...statusOptions]}
                  />
                )}
              />
              {v.tenderStatus === 'held' && (
                <div className='grid gap-4 sm:grid-cols-2'>
                  <form.AppField
                    name='tenderNoticeNumber'
                    children={(field) => (
                      <field.TextField label='Nº do edital' required maxLength={50} />
                    )}
                  />
                  <form.AppField
                    name='tenderApprovedOn'
                    children={(field) => (
                      <field.TextField label='Data da homologação' type='date' required />
                    )}
                  />
                </div>
              )}
              {(v.tenderStatus === 'waived' || v.tenderStatus === 'unfeasible') && (
                <>
                  <form.AppField
                    name='tenderLegalBasis'
                    children={(field) => (
                      <field.SelectField
                        label='Fundamento legal'
                        required
                        placeholder='Escolha'
                        options={[{ value: '', label: 'Escolha' }, ...basisOptions]}
                      />
                    )}
                  />
                  <form.AppField
                    name='tenderJustification'
                    children={(field) => (
                      <field.TextareaField
                        label='Justificativa'
                        required
                        rows={3}
                        maxLength={2000}
                        showCount
                      />
                    )}
                  />
                </>
              )}
            </fieldset>
          )}

          {hiring && (
            <fieldset className='space-y-4 rounded-lg border p-4'>
              <legend className='px-1 text-sm font-medium'>
                Shows e eventos musicais (Lei Estadual 12.082/2023)
              </legend>
              <p className='text-muted-foreground text-sm'>
                Repasse limitado ao teto vigente; cachês de no máximo 50% do repasse, sendo pelo
                menos 30% deles para artistas regionais; contrapartida mínima de 10% em convênio com
                município. Os limites exatos aparecem nas pendências.
              </p>
              <div className='grid gap-4 sm:grid-cols-2'>
                <form.Field
                  name='artistFeesCents'
                  children={(field) => (
                    <MoneyField
                      id='artist-fees'
                      label='Total de cachês artísticos'
                      required
                      value={field.state.value}
                      onChange={field.handleChange}
                    />
                  )}
                />
                <form.Field
                  name='regionalArtistFeesCents'
                  children={(field) => (
                    <MoneyField
                      id='regional-fees'
                      label='Cachês de artistas regionais'
                      description='Parte do total de cachês.'
                      value={field.state.value}
                      onChange={field.handleChange}
                    />
                  )}
                />
              </div>
              <form.AppField
                name='governorWaiver'
                children={(field) => (
                  <field.SwitchField
                    label='Há autorização expressa do Governador para afastar os limites'
                    description='Art. 5º da Lei 12.082/2023. O documento da autorização é anexado na etapa de documentos.'
                  />
                )}
              />
            </fieldset>
          )}

          <ServerErrors error={save.error} />
          <StepActions
            saving={save.isPending}
            disabled={readOnly}
            onSave={() => (advance.current = false)}
            onSaveAndContinue={() => (advance.current = true)}
            last
          />
        </FieldGroup>
      </ReadOnlyScope>
    </form>
  );
}
