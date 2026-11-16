'use client';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { Icons } from '@/components/icons';
import { Badge } from '@/components/ui/badge';
import { Button, buttonVariants } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { LoadingButton } from '@/components/ui/loading-button';
import { Progress } from '@/components/ui/progress';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { useCan } from '@/features/auth/session-provider';
import { errorMessage } from '@/lib/errors';
import { formatBytes } from '@/lib/utils';
import { projectKeys, reportsQueryOptions } from '../api/queries';
import {
  downloadReport,
  generateReport,
  reportDownloadUrl,
  requestFullReport,
  type ReportFile
} from '../api/service';
import {
  projectActions,
  reportKindLabels,
  reportStatusLabels,
  type Project,
  type ProjectReport,
  type ReportKind
} from '../api/types';
import { formatDateTimeCuiaba } from '../format';

/** Entrega o PDF recebido como download do navegador. */
function saveFile({ blob, filename }: ReportFile) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function ReportDialog({
  project,
  open,
  onOpenChange
}: {
  project: Project;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const [kind, setKind] = useState<ReportKind>('summary');
  const [pageNumbers, setPageNumbers] = useState(true);
  const [coverSheets, setCoverSheets] = useState(true);
  const generate = useMutation({
    mutationFn: async () => {
      if (kind === 'full') {
        await requestFullReport(project.id, { pageNumbers, coverSheets });
        return null;
      }
      return generateReport(project.id, { pageNumbers });
    },
    onSuccess: async (file) => {
      await qc.invalidateQueries({ queryKey: projectKeys.reports(project.id) });
      if (file) {
        saveFile(file);
        toast.success('Relatório gerado.');
      } else {
        toast.success('Relatório completo na fila. A montagem pode levar alguns minutos.');
      }
      onOpenChange(false);
    },
    onError: async (err) => {
      toast.error(errorMessage(err));
      // O motivo pode ter mudado (enquadramento recusado, projeto arquivado): atualiza a situação do projeto.
      await qc.invalidateQueries({ queryKey: projectKeys.detail(project.id) });
      await qc.invalidateQueries({ queryKey: projectKeys.reports(project.id) });
    }
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Gerar relatório do projeto</DialogTitle>
          <DialogDescription>
            PDF com as informações cadastradas no projeto, conforme a versão enviada. Quem gerou, a
            data e os dados usados ficam registrados no histórico.
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4'>
          <RadioGroup
            value={kind}
            onValueChange={(v) => setKind(v as ReportKind)}
            className='gap-3'
          >
            <div className='flex items-start gap-2'>
              <RadioGroupItem value='summary' id='report-kind-summary' />
              <Label htmlFor='report-kind-summary' className='flex flex-col items-start gap-0.5'>
                <span>Resumido (só as informações)</span>
                <span className='text-muted-foreground text-xs font-normal'>
                  Dados do cadastro e a lista de documentos, sem o conteúdo dos arquivos. Sai na
                  hora.
                </span>
              </Label>
            </div>
            <div className='flex items-start gap-2'>
              <RadioGroupItem value='full' id='report-kind-full' />
              <Label htmlFor='report-kind-full' className='flex flex-col items-start gap-0.5'>
                <span>Completo (com os documentos)</span>
                <span className='text-muted-foreground text-xs font-normal'>
                  Junta os documentos no mesmo PDF, com marcadores e sumário com links. É montado em
                  segundo plano e pode levar alguns minutos; o PDF fica disponível por 24 horas.
                </span>
              </Label>
            </div>
          </RadioGroup>
          <div className='space-y-2'>
            {kind === 'full' && (
              <div className='flex items-center gap-2'>
                <Checkbox
                  id='report-cover-sheets'
                  checked={coverSheets}
                  onCheckedChange={(v) => setCoverSheets(v === true)}
                />
                <Label htmlFor='report-cover-sheets'>Folha de rosto antes de cada documento</Label>
              </div>
            )}
            <div className='flex items-center gap-2'>
              <Checkbox
                id='report-page-numbers'
                checked={pageNumbers}
                onCheckedChange={(v) => setPageNumbers(v === true)}
              />
              <Label htmlFor='report-page-numbers'>
                Numerar as páginas{kind === 'full' ? ' (todo o PDF, inclusive os documentos)' : ''}
              </Label>
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            Cancelar
          </Button>
          <LoadingButton loading={generate.isPending} onClick={() => generate.mutate()}>
            <Icons.fileTypePdf aria-hidden />{' '}
            {kind === 'full' ? 'Montar relatório completo' : 'Gerar relatório'}
          </LoadingButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const mib = (bytes: number) => `${Math.round(bytes / (1 << 20))} MB`;

function ReportStatusBadge({ report }: { report: ProjectReport }) {
  const variant =
    report.status === 'failed' ? 'destructive' : report.status === 'done' ? 'secondary' : 'outline';
  return <Badge variant={variant}>{reportStatusLabels[report.status]}</Badge>;
}

function HistoryRow({ projectId, report }: { projectId: string; report: ProjectReport }) {
  const again = useMutation({
    mutationFn: () => downloadReport(projectId, report.id),
    onSuccess: saveFile,
    onError: (err) => toast.error(errorMessage(err))
  });
  const isFull = report.kind === 'full';
  const active = report.status === 'queued' || report.status === 'running';
  const percent =
    report.progressTotal > 0 ? Math.round((report.progressDone / report.progressTotal) * 100) : 0;
  return (
    <li className='space-y-2 py-2 text-sm'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <p className='flex flex-wrap items-center gap-2'>
            <span className='font-medium'>{reportKindLabels[report.kind]}</span>
            {isFull && <ReportStatusBadge report={report} />}
            <span>{formatDateTimeCuiaba(report.generatedAt)}</span>
          </p>
          <p className='text-muted-foreground text-xs'>
            {report.generatedBy} · versão {report.versionNumber} do projeto
            {report.status === 'done' && (
              <>
                {' · '}
                {report.pageCount} {report.pageCount === 1 ? 'página' : 'páginas'} ·{' '}
                {formatBytes(report.sizeBytes)}
              </>
            )}
            {!report.pageNumbers && ' · sem numeração'}
            {isFull && !report.coverSheets && ' · sem folha de rosto'}
          </p>
        </div>
        {report.status === 'done' &&
          (isFull ? (
            report.downloadable ? (
              <a
                href={reportDownloadUrl(projectId, report.id)}
                download
                className={buttonVariants({ variant: 'outline', size: 'sm' })}
              >
                <Icons.download aria-hidden /> Baixar
              </a>
            ) : (
              <span className='text-muted-foreground text-xs'>
                PDF vencido: gere de novo para baixar
              </span>
            )
          ) : (
            <LoadingButton
              variant='outline'
              size='sm'
              loading={again.isPending}
              onClick={() => again.mutate()}
            >
              <Icons.download aria-hidden /> Baixar de novo
            </LoadingButton>
          ))}
      </div>
      {active && (
        <div className='space-y-1'>
          <Progress value={report.status === 'queued' ? 0 : percent} />
          <p className='text-muted-foreground text-xs'>
            {report.status === 'queued'
              ? 'Aguardando a vez na fila de montagem.'
              : `Preparando os arquivos: ${report.progressDone} de ${report.progressTotal}.`}{' '}
            Você pode sair desta tela; a montagem continua.
          </p>
        </div>
      )}
      {report.status === 'failed' && report.error && (
        <p className='text-destructive text-xs'>{report.error}</p>
      )}
      {report.warnings.length > 0 && (
        <details className='text-xs'>
          <summary className='cursor-pointer text-amber-700 dark:text-amber-400'>
            {report.warnings.length}{' '}
            {report.warnings.length === 1 ? 'arquivo não entrou' : 'arquivos não entraram'} no PDF
          </summary>
          <ul className='text-muted-foreground mt-1 list-disc space-y-0.5 pl-5'>
            {report.warnings.map((w, i) => (
              <li key={`${w.file}-${i}`}>
                <span className='font-medium'>{w.document}</span> ({w.file}): {w.reason}
              </li>
            ))}
          </ul>
        </details>
      )}
      {isFull && report.stats && report.status === 'done' && (
        <p className='text-muted-foreground text-xs'>
          Montagem: {Math.max(1, Math.round(report.stats.durationMs / 1000))} s · pico de memória da
          API {mib(report.stats.peakRssBytes)} (em repouso {mib(report.stats.baselineRssBytes)})
        </p>
      )}
    </li>
  );
}

function ReportHistory({ projectId }: { projectId: string }) {
  const { data } = useQuery(reportsQueryOptions(projectId));
  if (!data || data.length === 0) return null;
  return (
    <div className='space-y-1'>
      <h4 className='text-sm font-medium'>Relatórios gerados</h4>
      <ul className='divide-y'>
        {data.map((r) => (
          <HistoryRow key={r.id} projectId={projectId} report={r} />
        ))}
      </ul>
    </div>
  );
}

/**
 * Relatório do projeto (PDF): botão para gerar e histórico de quem gerou. Só aparece para quem tem a ação e em projeto já enviado; o
 * botão fica desabilitado com o motivo enquanto o enquadramento não foi confirmado (a regra é do servidor).
 */
export function ProjectReportCard({ project }: { project: Project }) {
  const can = useCan();
  const [open, setOpen] = useState(false);
  const access = project.report;
  if (!access || !can(projectActions.report)) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>Relatório do projeto</CardTitle>
        <CardDescription>
          PDF com as informações cadastradas, resumido ou completo (com os documentos juntados).
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='flex flex-wrap items-center gap-3'>
          <Button disabled={!access.available} onClick={() => setOpen(true)}>
            <Icons.fileTypePdf aria-hidden /> Gerar relatório
          </Button>
          {!access.available && access.reason && (
            <p className='text-muted-foreground text-sm'>{access.reason}</p>
          )}
        </div>
        <ReportHistory projectId={project.id} />
      </CardContent>
      {open && <ReportDialog project={project} open={open} onOpenChange={setOpen} />}
    </Card>
  );
}
