'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { QueryError } from '@/components/query-error';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { LoadingButton } from '@/components/ui/loading-button';
import { Skeleton } from '@/components/ui/skeleton';
import { projectKeys, sigadocQueryOptions } from '../../api/queries';
import { markSigadocSeen } from '../../api/service';
import type { Project, SigadocMovement } from '../../api/types';
import { formatDateTimeCuiaba } from '../../format';

function when(m: SigadocMovement) {
  return m.at ? formatDateTimeCuiaba(m.at) : m.atRaw || 'data não informada';
}

function MovementItem({ m }: { m: SigadocMovement }) {
  return (
    <li className='border-border border-b py-2 last:border-b-0'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-sm font-medium'>{m.event}</span>
        {m.afterArchive && <Badge variant='destructive'>Depois do arquivamento</Badge>}
        {m.afterArchive && !m.seen && <Badge variant='outline'>Nova</Badge>}
      </div>
      {m.description && <p className='text-muted-foreground text-sm'>{m.description}</p>}
      <p className='text-muted-foreground text-xs'>
        {when(m)}
        {m.person && ` · ${m.person}`}
        {m.lotacao && ` · de ${m.lotacao}`}
        {m.destination && m.destination !== m.lotacao && ` para ${m.destination}`}
      </p>
    </li>
  );
}

/**
 * A tramitação do processo no SIGADOC, como o sistema a acompanha. Só leitura: parecer, despachos e tramitações são feitos no
 * SIGADOC. Sem processo, o painel não aparece.
 */
export function SigadocTimeline({ projectId }: { projectId: string }) {
  const q = useQuery(sigadocQueryOptions(projectId));
  if (q.isLoading) return <Skeleton className='h-24 w-full' />;
  if (q.isError) {
    return (
      <Card>
        <CardContent className='pt-6'>
          <QueryError error={q.error} what='a tramitação no SIGADOC' />
        </CardContent>
      </Card>
    );
  }
  const view = q.data;
  if (!view?.process) return null;
  const p = view.process;
  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex flex-wrap items-center gap-2 text-base'>
          Tramitação no SIGADOC
          <Badge variant='secondary' className='font-mono'>
            {p.number}
          </Badge>
          {p.archivedAtSigadoc && <Badge variant='outline'>Arquivado no SIGADOC</Badge>}
        </CardTitle>
      </CardHeader>
      <CardContent className='space-y-3'>
        <p className='text-muted-foreground text-xs'>
          O sistema só acompanha o processo: tudo o que acontece nele é feito diretamente no
          SIGADOC.
          {p.lastCheckedAt && <> Última consulta em {formatDateTimeCuiaba(p.lastCheckedAt)}.</>}
          {p.situation && <> Situação: {p.situation}.</>}
        </p>
        {p.error && (
          <Alert variant='destructive'>
            <Icons.alertCircle aria-hidden />
            <AlertTitle>A última consulta ao SIGADOC falhou</AlertTitle>
            <AlertDescription>
              {p.error} O sistema tenta de novo sozinho; o que já foi registrado continua valendo.
            </AlertDescription>
          </Alert>
        )}
        {view.movements.length === 0 ? (
          <p className='text-muted-foreground text-sm'>Nenhuma movimentação registrada ainda.</p>
        ) : (
          <ul>
            {view.movements.map((m) => (
              <MovementItem key={m.id} m={m} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

/**
 * Aviso chamativo do projeto arquivado no sistema cujo processo segue sem arquivamento no SIGADOC, com o que foi acrescentado depois do
 * arquivamento. Quando o SIGADOC também arquiva, informa "arquivado nos dois".
 */
export function SigadocArchiveNotice({ project }: { project: Project }) {
  const qc = useQueryClient();
  const enabled = project.status === 'archived';
  const q = useQuery({ ...sigadocQueryOptions(project.id), enabled });
  const seen = useMutation({
    mutationFn: () => markSigadocSeen(project.id),
    onSuccess: (view) => {
      qc.setQueryData(projectKeys.sigadoc(project.id), view);
      toast.success('Movimentações marcadas como vistas.');
    }
  });
  const view = q.data;
  if (!enabled || !view?.process) return null;
  const after = view.movements.filter((m) => m.afterArchive);

  if (!view.stillOpenAtSigadoc) {
    return (
      <Alert>
        <Icons.circleCheck aria-hidden />
        <AlertTitle>Arquivado no sistema e no SIGADOC</AlertTitle>
        <AlertDescription>
          O processo <span className='font-mono'>{view.process.number}</span> também foi arquivado
          no SIGADOC.
          {after.length > 0 &&
            ` Houve ${after.length} movimentação(ões) depois do arquivamento no sistema.`}
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <Alert variant='destructive'>
      <Icons.warning aria-hidden />
      <AlertTitle>
        Projeto arquivado no sistema, mas o processo segue sem arquivamento no SIGADOC
      </AlertTitle>
      <AlertDescription className='space-y-2'>
        <p>
          O sistema não arquiva o processo <span className='font-mono'>{view.process.number}</span>{' '}
          no SIGADOC: ele continua ativo lá e o sistema segue acompanhando.
        </p>
        {after.length === 0 ? (
          <p>Nada foi acrescentado ao processo depois do arquivamento no sistema.</p>
        ) : (
          <>
            <p className='font-medium'>
              Acrescentado no SIGADOC depois do arquivamento ({after.length}
              {view.newAfterArchive > 0 ? `, ${view.newAfterArchive} nova(s)` : ''}):
            </p>
            <ul className='list-disc space-y-1 pl-5'>
              {after.map((m) => (
                <li key={m.id}>
                  <span className='font-medium'>{m.event}</span>
                  {m.description ? `: ${m.description}` : ''} ({when(m)}
                  {m.person ? `, ${m.person}` : ''})
                </li>
              ))}
            </ul>
            {view.newAfterArchive > 0 && (
              <LoadingButton
                size='sm'
                variant='outline'
                loading={seen.isPending}
                onClick={() => seen.mutate()}
              >
                Marcar como vistas
              </LoadingButton>
            )}
          </>
        )}
      </AlertDescription>
    </Alert>
  );
}
