import type { SectionKey } from './api/types';

const reasonTexts: Record<string, string> = {
  signer_role: 'o cargo de quem assina',
  signer_name: 'o nome de quem assina',
  signer_cpf: 'o CPF de quem assina',
  text: 'dados que aparecem no texto da declaração',
  place: 'o município ou o nome da instituição',
  template: 'o modelo oficial da declaração'
};

/** "o cargo de quem assina e o nome de quem assina"; vazio se não há motivo conhecido. */
export function staleReasonsText(reasons: string[] | undefined): string {
  const texts = (reasons ?? []).map((r) => reasonTexts[r]).filter(Boolean);
  if (texts.length <= 1) return texts[0] ?? '';
  return `${texts.slice(0, -1).join(', ')} e ${texts[texts.length - 1]}`;
}

/** A frase do aviso de declaração desatualizada, dizendo o que mudou quando se sabe. */
export function staleMessage(reasons: string[] | undefined, signed: boolean): string {
  const what = staleReasonsText(reasons);
  if (signed) {
    return `${what ? `Mudou: ${what}.` : 'O cadastro mudou.'} A declaração assinada deixou de valer; gere e assine de novo.`;
  }
  return `${what ? `Depois deste pedido mudou: ${what}.` : 'O cadastro mudou depois deste pedido.'} Gere a declaração de novo para assinar o texto atualizado.`;
}

/** Etapa do cadastro onde se corrige a pendência de uma declaração (o campo vem do servidor). */
export function stepOfIssue(field: string): SectionKey {
  if (/^(personal|legalRep|isLegalRepresentative)/.test(field)) return 'proponent';
  if (/^(cnpj|institutionName|entityTypeCode|municipalityIbge)/.test(field)) return 'institution';
  if (/^(counterpart|funding)/.test(field)) return 'funding';
  return 'proposal';
}
