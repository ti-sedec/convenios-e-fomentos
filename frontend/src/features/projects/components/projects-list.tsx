'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import {
  debounce,
  parseAsInteger,
  parseAsString,
  parseAsStringLiteral,
  useQueryStates
} from 'nuqs';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { Pager } from '@/components/pager';
import { AlertModal } from '@/components/modal/alert-modal';
import { QueryError } from '@/components/query-error';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button, buttonVariants } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table';
import { useCan } from '@/features/auth/session-provider';
import { overviewQueryOptions } from '@/features/proponent-overview/api/queries';
import { useDebounce } from '@/hooks/use-debounce';
import { errorMessage } from '@/lib/errors';
import { profileQueryOptions, projectKeys, projectsQueryOptions } from '../api/queries';
import { deleteProject } from '../api/service';
import {
  accessLabels,
  archiveReasonLabels,
  instrumentLabels,
  projectActions,
  stageLabels,
  statusLabels,
  type ProjectSummary
} from '../api/types';
import { accessApi, accessKeys } from '../access-filter';
import { situationByKey, situations } from '../situations';
import { AccessFilter } from './access-filter';
import { DuplicateButton } from './project-actions';
import { formatBrl, formatCnpj } from '../format';

const dateTime = new Intl.DateTimeFormat('pt-BR', {
  dateStyle: 'short',
  timeStyle: 'short',
  timeZone: 'America/Cuiaba'
});

export function ProjectsList() {
  const can = useCan();
  const qc = useQueryClient();
  const [filters, setFilters] = useQueryStates(
    {
      acesso: parseAsStringLiteral(accessKeys).withDefault('todos'),
      situacao: parseAsString.withDefault(''),
      q: parseAsString.withDefault(''),
      page: parseAsInteger.withDefault(1)
    },
    { clearOnDefault: true, limitUrlUpdates: debounce(300) }
  );
  const q = useDebounce(filters.q.trim(), 300);
  const access = accessApi(filters.acesso);
  const situation = situationByKey(filters.situacao);
  const list = useQuery({
    ...projectsQueryOptions({ ...situation?.params, access, q, page: filters.page }),
    placeholderData: (prev) => prev
  });
  // Os números do seletor e das situações vêm do resumo (o mesmo da visão geral), não de uma consulta por botão.
  const overview = useQuery(overviewQueryOptions(access));
  const totals = useQuery({ ...overviewQueryOptions(), select: (o) => o.byAccess });
  const profile = useQuery({ ...profileQueryOptions(), enabled: can(projectActions.profileRead) });
  const [toDelete, setToDelete] = useState<ProjectSummary | null>(null);

  const remove = useMutation({
    mutationFn: deleteProject,
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: projectKeys.all });
      toast.success('Rascunho excluído.');
      setToDelete(null);
    },
    onError: (err) => {
      toast.error(errorMessage(err));
      setToDelete(null);
    }
  });

  if (list.isError) return <QueryError error={list.error} what='os projetos' />;
  if (list.isLoading) return <Skeleton className='h-60 w-full' />;
  const rows = list.data?.data ?? [];
  const meta = list.data?.meta;

  return (
    <div className='space-y-4'>
      {profile.data && !profile.data.complete && (
        <Alert>
          <AlertDescription>
            Complete o seu{' '}
            <Link
              href='/dashboard/perfil-proponente'
              className='font-medium underline underline-offset-4'
            >
              perfil de proponente
            </Link>{' '}
            (celular, e-mail, município e cargo) para que os novos projetos já venham com os seus
            contatos.
          </AlertDescription>
        </Alert>
      )}
      <AccessFilter
        value={filters.acesso}
        onChange={(acesso) => void setFilters({ acesso, page: 1 })}
        totals={totals.data}
      />
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          size='sm'
          variant={situation ? 'outline' : 'secondary'}
          aria-pressed={!situation}
          onClick={() => void setFilters({ situacao: '', page: 1 })}
        >
          Todas as situações
        </Button>
        {situations.map((sit) => (
          <Button
            key={sit.key}
            size='sm'
            variant={situation?.key === sit.key ? 'secondary' : 'outline'}
            aria-pressed={situation?.key === sit.key}
            onClick={() => void setFilters({ situacao: sit.key, page: 1 })}
          >
            {sit.label}
            {overview.data && (
              <Badge variant='secondary' className='ml-1 tabular-nums'>
                {sit.count(overview.data.counts)}
              </Badge>
            )}
          </Button>
        ))}
      </div>
      <Input
        value={filters.q}
        onChange={(e) => void setFilters({ q: e.target.value, page: 1 })}
        placeholder='Buscar por título, instituição, CNPJ ou nº do SIGCon'
        aria-label='Buscar projetos'
        className='max-w-md'
      />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Projeto</TableHead>
            <TableHead>Instituição</TableHead>
            <TableHead>Instrumento</TableHead>
            <TableHead>Valor solicitado</TableHead>
            <TableHead>Situação</TableHead>
            <TableHead>Atualizado em</TableHead>
            <TableHead className='w-px' />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className='text-muted-foreground text-center'>
                {q || situation || filters.acesso !== 'todos'
                  ? 'Nenhum projeto encontrado com estes filtros.'
                  : 'Você ainda não tem projetos. Use "Novo projeto" para começar.'}
              </TableCell>
            </TableRow>
          )}
          {rows.map((p) => (
            <TableRow key={p.id}>
              <TableCell>
                <Link
                  href={`/dashboard/projetos/${p.id}`}
                  className='font-medium underline-offset-4 hover:underline'
                >
                  {p.title || 'Projeto sem título'}
                </Link>
                {p.code && <div className='text-muted-foreground font-mono text-xs'>{p.code}</div>}
                {p.access && p.access !== 'owner' && (
                  <Badge variant='outline' className='mt-1'>
                    Compartilhado · {accessLabels[p.access]}
                  </Badge>
                )}
                {p.sigconNumber && (
                  <div className='text-muted-foreground text-xs'>SIGCon {p.sigconNumber}</div>
                )}
              </TableCell>
              <TableCell>
                {p.institutionName || <span className='text-muted-foreground'>—</span>}
                {p.institutionCnpj && (
                  <div className='text-muted-foreground text-xs'>
                    {formatCnpj(p.institutionCnpj)}
                  </div>
                )}
              </TableCell>
              <TableCell>
                {p.instrument ? (
                  instrumentLabels[p.instrument]
                ) : (
                  <span className='text-muted-foreground'>—</span>
                )}
              </TableCell>
              <TableCell className='tabular-nums'>
                {p.totalCents ? (
                  formatBrl(p.totalCents)
                ) : (
                  <span className='text-muted-foreground'>—</span>
                )}
              </TableCell>
              <TableCell>
                <div className='flex flex-col items-start gap-1'>
                  <Badge variant={p.status === 'archived' ? 'outline' : 'secondary'}>
                    {statusLabels[p.status]}
                  </Badge>
                  {p.status === 'submitted' && p.stage && (
                    <span className='text-muted-foreground text-xs'>{stageLabels[p.stage]}</span>
                  )}
                  {p.status === 'submitted' && p.complementDueAt && (
                    <span className='text-xs font-medium'>
                      Prazo das correções: {dateTime.format(new Date(p.complementDueAt))}
                    </span>
                  )}
                  {p.status === 'archived' && p.archiveReason && (
                    <span className='text-muted-foreground text-xs'>
                      {archiveReasonLabels[p.archiveReason]}
                    </span>
                  )}
                </div>
              </TableCell>
              <TableCell className='tabular-nums'>
                {dateTime.format(new Date(p.updatedAt))}
              </TableCell>
              <TableCell>
                <div className='flex justify-end gap-1'>
                  <Link
                    href={`/dashboard/projetos/${p.id}`}
                    className={buttonVariants({ size: 'sm', variant: 'ghost' })}
                  >
                    <Icons.edit aria-hidden /> Abrir
                  </Link>
                  <DuplicateButton project={p} variant='ghost' />
                  {p.isOwner && p.status === 'draft' && can(projectActions.delete) && (
                    <Button size='sm' variant='ghost' onClick={() => setToDelete(p)}>
                      <Icons.trash aria-hidden /> Excluir
                    </Button>
                  )}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Pager meta={meta} onPage={(page) => void setFilters({ page })} />
      <AlertModal
        isOpen={toDelete !== null}
        onClose={() => setToDelete(null)}
        onConfirm={() => toDelete && remove.mutate(toDelete.id)}
        loading={remove.isPending}
        title={`Excluir o rascunho "${toDelete?.title || 'sem título'}"?`}
        description='Todos os dados preenchidos serão apagados. A exclusão não pode ser desfeita.'
        confirmLabel='Excluir'
        cancelLabel='Cancelar'
      />
    </div>
  );
}
