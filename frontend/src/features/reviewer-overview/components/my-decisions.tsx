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
import { reviewerMySummaryQueryOptions } from '../api/queries';
import type { ReviewerMyCounts } from '../api/types';

const rows: { key: keyof ReviewerMyCounts; label: string }[] = [
  { key: 'confirmed', label: 'Enquadramentos confirmados' },
  { key: 'refused', label: 'Enquadramentos recusados' }
];

/** O que a própria pessoa decidiu nos últimos 7 e 30 dias. Só ela vê estes números; não compara com ninguém. */
export function MyDecisions() {
  const me = useQuery(reviewerMySummaryQueryOptions());
  return (
    <Card>
      <CardHeader>
        <CardTitle>Minhas decisões</CardTitle>
        <CardDescription>Só você vê estes números.</CardDescription>
      </CardHeader>
      <CardContent>
        {me.isError ? (
          <QueryError error={me.error} what='as suas decisões' />
        ) : me.isLoading || !me.data ? (
          <Skeleton className='h-24 w-full' />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>O que você decidiu</TableHead>
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
