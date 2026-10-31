'use client';

import { useQuery } from '@tanstack/react-query';
import { QueryError } from '@/components/query-error';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/ui/table';
import { mySummaryQueryOptions } from '../api/queries';
import type { MyCounts } from '../api/types';

const rows: { key: keyof MyCounts; label: string }[] = [
  { key: 'assumed', label: 'Análises assumidas' },
  { key: 'complements', label: 'Complementações pedidas' },
  { key: 'indicated', label: 'Departamentos indicados' },
  { key: 'archived', label: 'Projetos arquivados' }
];

/** O que a própria pessoa fez nos últimos 7 e 30 dias. Só ela vê estes números; não compara com ninguém. */
export function MySummary() {
  const me = useQuery(mySummaryQueryOptions());
  return (
    <Card>
      <CardHeader>
        <CardTitle>Meu resumo</CardTitle>
        <CardDescription>Só você vê estes números.</CardDescription>
      </CardHeader>
      <CardContent>
        {me.isError ? (
          <QueryError error={me.error} what='o seu resumo' />
        ) : me.isLoading || !me.data ? (
          <Skeleton className='h-32 w-full' />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>O que você fez</TableHead>
                <TableHead className='text-right'>7 dias</TableHead>
                <TableHead className='text-right'>30 dias</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.key}>
                  <TableCell>{r.label}</TableCell>
                  <TableCell className='text-right tabular-nums'>{me.data.last7[r.key]}</TableCell>
                  <TableCell className='text-right tabular-nums'>{me.data.last30[r.key]}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
