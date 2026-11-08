import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Icons } from '@/components/icons';

/** O Revisor sem departamento no cadastro não enxerga nenhum projeto: a tela diz o motivo em vez de ficar vazia. */
export function NoDepartment() {
  return (
    <Alert>
      <Icons.alertCircle />
      <AlertTitle>Seu usuário ainda não tem um departamento</AlertTitle>
      <AlertDescription>
        O Revisor do Departamento só enxerga os projetos do departamento a que pertence. Peça à
        administração para vinculá-lo ao seu; até lá, nenhum projeto aparece aqui.
      </AlertDescription>
    </Alert>
  );
}
