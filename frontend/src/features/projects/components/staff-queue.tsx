'use client';

import { useQueries, useQuery } from '@tanstack/react-query';
import Link from 'next/link';
import { debounce, parseAsInteger, parseAsString, useQueryStates } from 'nuqs';
import { useMemo } from 'react';
import { Icons } from '@/components/icons';
import { Pager } from '@/components/pager';
import { QueryError } from '@/components/query-error';
import { Badge } from '@/components/ui/badge';
import { buttonVariants } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { departmentsQueryOptions } from '@/features/departments/api/queries';
import { departmentActions } from '@/features/departments/api/types';
import { useCan } from '@/features/auth/session-provider';
import { useDebounce } from '@/hooks/use-debounce';
import { projectsQueryOptions } from '../api/queries';
import {
  archiveReasonLabels,
  framingStatusLabels,
  instrumentLabels,
  stageLabels,
  statusLabels,
  type ProjectSummary
} from '../api/types';
import { formatBrl, formatCnpj, formatDateTimeCuiaba, formatDuration } from '../format';
import { queueTabsFor } from '../queues';

const ALL = 'all';

/** "Parado há": o mínimo de dias corridos na etapa atual. Vazio = sem filtro. */
const idleOptions = [
  { value: ALL, label: 'Qualquer tempo na etapa' },
  { value: '3', label: 'Na etapa há 3 dias ou mais' },
  { value: '7', label: 'Na etapa há 7 dias ou mais' },
  { value: '15', label: 'Na etapa há 15 dias ou mais' },
  { value: '30', label: 'Na etapa há 30 dias ou mais' }
];

const orderOptions = [
  { value: 'recentes', label: 'Atualizados há menos tempo' },
  { value: 'parados', label: 'Há mais tempo na etapa' }
];

/** A situação do projeto na fila: etapa e, quando há, enquadramento e departamento. Sempre em texto, nunca só cor. */
function Situation({ p }: { p: ProjectSummary }) {
  return (
    <div className='flex flex-col items-start gap-1'>
      <Badge variant={p.status === 'archived' ? 'outline' : 'secondary'}>
        {statusLabels[p.status]}
      </Badge>
      {p.status === 'submitted' && p.stage && (
        <span className='text-muted-foreground text-xs'>{stageLabels[p.stage]}</span>
      )}
      {p.status === 'submitted' && p.framingStatus && (
        <span className='text-xs font-medium'>{framingStatusLabels[p.framingStatus]}</span>
      )}
      {p.department && p.status !== 'draft' && (
        <span className='text-muted-foreground text-xs'>Departamento: {p.department.name}</span>
      )}
      {p.status === 'submitted' && p.complementDueAt && (
        <span className='text-xs font-medium'>
          Prazo das correções: {formatDateTimeCuiaba(p.complementDueAt)}
        </span>
      )}
      {p.status === 'archived' && p.archiveReason && (
        <span className='text-muted-foreground text-xs'>
          {archiveReasonLabels[p.archiveReason]}
        </span>
      )}
    </div>
  );
}

/** Fila de trabalho da equipe: uma aba por situação, conforme o perfil. */
export function StaffQueue() {
  const can = useCan();
  const tabs = useMemo(() => queueTabsFor(can), [can]);
  const [filters, setFilters] = useQueryStates(
    {
      aba: parseAsString.withDefault(''),
      q: parseAsString.withDefault(''),
      departamento: parseAsString.withDefault(''),
      parado: parseAsString.withDefault(''),
      ordem: parseAsString.withDefault('recentes'),
      page: parseAsInteger.withDefault(1)
    },
    { clearOnDefault: true, limitUrlUpdates: debounce(300) }
  );
  const current = tabs.find((t) => t.key === filters.aba) ?? tabs[0];
  const search = useDebounce(filters.q, 300);

  const list = useQuery({
    ...projectsQueryOptions({
      ...current.params,
      q: search,
      department: filters.departamento || undefined,
      idleDays: filters.parado ? Number(filters.parado) : undefined,
      sort: filters.ordem === 'parados' ? 'stage_since:asc' : undefined,
      page: filters.page
    }),
    placeholderData: (prev) => prev
  });
  // Tamanho de cada fila (sem a busca nem o filtro de departamento): uma consulta pequena por aba.
  const counts = useQueries({
    queries: tabs.map((t) => ({
      ...projectsQueryOptions({ ...t.params, pageSize: 1 }),
      select: (r: { meta?: { total: number } }) => r.meta?.total ?? 0
    }))
  });
  const canFilterByDepartment = can(departmentActions.read);
  const departments = useQuery({ ...departmentsQueryOptions(), enabled: canFilterByDepartment });
  const departmentItems = [
    { value: ALL, label: 'Todos os departamentos' },
    ...(departments.data ?? []).map((d) => ({ value: d.id, label: d.name }))
  ];

  const rows = list.data?.data ?? [];

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <Input
          value={filters.q}
          onChange={(e) => void setFilters({ q: e.target.value, page: 1 })}
          placeholder='Buscar por título, instituição, CNPJ ou nº do SIGCon…'
          autoComplete='off'
          aria-label='Buscar projetos'
          className='max-w-md'
        />
        {canFilterByDepartment && departmentItems.length > 1 && (
          <Select
            items={departmentItems}
            value={filters.departamento || ALL}
            onValueChange={(v) =>
              void setFilters({ departamento: v === ALL ? '' : (v ?? ''), page: 1 })
            }
          >
            <SelectTrigger aria-label='Filtrar por departamento'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {departmentItems.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        )}
        <Select
          items={idleOptions}
          value={filters.parado || ALL}
          onValueChange={(v) => void setFilters({ parado: v === ALL ? '' : (v ?? ''), page: 1 })}
        >
          <SelectTrigger aria-label='Filtrar por tempo na etapa'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {idleOptions.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <Select
          items={orderOptions}
          value={filters.ordem}
          onValueChange={(v) => void setFilters({ ordem: v ?? 'recentes', page: 1 })}
        >
          <SelectTrigger aria-label='Ordenar a fila'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {orderOptions.map((o) => (
                <SelectItem key={o.value} value={o.value}>
                  {o.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      <Tabs value={current.key} onValueChange={(v) => void setFilters({ aba: v, page: 1 })}>
        <TabsList className='h-auto flex-wrap justify-start'>
          {tabs.map((t, i) => (
            <TabsTrigger key={t.key} value={t.key}>
              {t.label}
              {counts[i]?.data !== undefined && (
                <Badge variant='secondary' className='ml-1 tabular-nums'>
                  {counts[i].data}
                </Badge>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value={current.key} className='space-y-4'>
          {list.isError ? (
            <QueryError error={list.error} what='os projetos' />
          ) : list.isLoading ? (
            <Skeleton className='h-60 w-full' />
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Projeto</TableHead>
                    <TableHead>Instituição</TableHead>
                    <TableHead>Instrumento</TableHead>
                    <TableHead>Valor solicitado</TableHead>
                    <TableHead>Situação</TableHead>
                    <TableHead>Analista</TableHead>
                    <TableHead>Na etapa há</TableHead>
                    <TableHead>Atualizado em</TableHead>
                    <TableHead className='w-px' />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={9} className='text-muted-foreground text-center'>
                        {search || filters.departamento || filters.parado
                          ? 'Nenhum projeto encontrado com estes filtros.'
                          : 'Nenhum projeto nesta fila.'}
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
                        {p.code && (
                          <div className='text-muted-foreground font-mono text-xs'>{p.code}</div>
                        )}
                        {p.sigconNumber && (
                          <div className='text-muted-foreground text-xs'>
                            SIGCon {p.sigconNumber}
                          </div>
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
                        <Situation p={p} />
                      </TableCell>
                      <TableCell>
                        {p.analyst?.name ?? <span className='text-muted-foreground'>—</span>}
                      </TableCell>
                      <TableCell className='tabular-nums'>
                        {p.stageSeconds === undefined ? (
                          <span className='text-muted-foreground'>—</span>
                        ) : (
                          formatDuration(p.stageSeconds)
                        )}
                      </TableCell>
                      <TableCell className='tabular-nums'>
                        {formatDateTimeCuiaba(p.updatedAt)}
                      </TableCell>
                      <TableCell>
                        <Link
                          href={`/dashboard/projetos/${p.id}`}
                          className={buttonVariants({ size: 'sm', variant: 'ghost' })}
                        >
                          <Icons.eye aria-hidden /> Abrir
                        </Link>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Pager meta={list.data?.meta} onPage={(page) => void setFilters({ page })} />
            </>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
