package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/logging"
	"github.com/wagner/convenios-e-fomentos/backend/internal/sigadoc"
)

// sigadocCheck autentica no SIGADOC (pelo X-Via) e mostra quais métodos o usuário de sistema consegue chamar. Não precisa de banco e
// não altera nada no SIGADOC: usa uma sigla inexistente, não cria documento e não envia PIN. Serve para conferir o que a MTI já
// liberou (docs/planejamento-integracao-sigadoc.md, seção 3).
func sigadocCheck(ctx context.Context, cfg *config.Config, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sigadoc:check", flag.ContinueOnError)
	matricula := fs.String("matricula", "", "matrícula SIGADOC a representar, ex.: SEDEC123456 (obrigatório)")
	strict := fs.Bool("strict", false, "falha (código 1) se algum método obrigatório do fluxo estiver bloqueado ou não implantado")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*matricula) == "" {
		return errors.New("informe --matricula (ex.: --matricula SEDEC123456)")
	}
	if cfg.SigadocURL() == "" || cfg.XRoadClient == "" || cfg.SigadocUsername == "" || cfg.SigadocPassword == "" {
		return errors.New("defina X_VIA_SEDEC_BASE_URL, X_ROAD_CLIENT, SIGADOC_USERNAME e SIGADOC_PASSWORD")
	}
	c, err := sigadoc.NewHTTP(sigadoc.HTTPConfig{
		BaseURL: cfg.SigadocURL(), XRoadClient: cfg.XRoadClient,
		Username: cfg.SigadocUsername, Password: cfg.SigadocPassword,
		CAFile: cfg.XViaCAFile, TLSServerName: cfg.XViaTLSServerName, Timeout: cfg.XViaTimeout,
		Log: logging.New(os.Stderr, "warn"),
	})
	if err != nil {
		return err
	}
	tok, err := c.Authenticate(ctx, *matricula)
	if err != nil {
		switch {
		case sigadoc.IsAuthFailed(err):
			return fmt.Errorf("o SIGADOC recusou o usuário de sistema (%w): confira SIGADOC_USERNAME e SIGADOC_PASSWORD", err)
		case sigadoc.IsUnknownIdentity(err):
			return fmt.Errorf("o SIGADOC não conhece a matrícula %q (use o formato SEDEC123456, em maiúsculas)", *matricula)
		}
		return err
	}
	exp := "sem vencimento informado"
	if !tok.ExpiresAt.IsZero() {
		exp = "vale até " + tok.ExpiresAt.Local().Format("02/01/2006 15:04")
	}
	say(out, "Autenticação OK para %s (%s).\n\n", strings.TrimSpace(*matricula), exp)

	outcomes := c.Check(ctx, tok, time.Now())
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "%s\n", "ETAPA\tMÉTODO\tCAMINHO\tRESULTADO\tHTTP\tDETALHE")
	counts := map[sigadoc.Result]int{}
	var missing, skipped []string
	for _, o := range outcomes {
		counts[o.Result]++
		status := "-"
		if o.Status != 0 {
			status = fmt.Sprint(o.Status)
		}
		say(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", o.Probe.Step, o.Probe.Method, o.Probe.Display(), label(o.Result), status, o.Detail)
		if o.Probe.Required && o.Result == sigadoc.Skipped {
			skipped = append(skipped, o.Probe.Method+" "+o.Probe.Display())
		}
		if o.Probe.Required && (o.Result == sigadoc.Blocked || o.Result == sigadoc.NotDeployed) {
			missing = append(missing, o.Probe.Method+" "+o.Probe.Display())
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	say(out, "\nLiberados: %d · Bloqueados: %d · Não implantados: %d · Sem resposta: %d · Não sondados: %d\n",
		counts[sigadoc.Allowed], counts[sigadoc.Blocked], counts[sigadoc.NotDeployed], counts[sigadoc.Unknown], counts[sigadoc.Skipped])
	if len(skipped) > 0 {
		say(out, "\nObrigatórios que a sondagem não testa (confirmar com a MTI):\n")
		for _, m := range skipped {
			say(out, "  - %s\n", m)
		}
	}
	if len(missing) > 0 {
		say(out, "\nObrigatórios para o fluxo e ainda sem acesso (pedir à MTI):\n")
		for _, m := range missing {
			say(out, "  - %s\n", m)
		}
		if *strict {
			return fmt.Errorf("%d método(s) obrigatório(s) sem acesso", len(missing))
		}
	}
	return nil
}

// say escreve no terminal; um erro de escrita num relatório de console não tem o que fazer.
func say(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }

func label(r sigadoc.Result) string {
	switch r {
	case sigadoc.Allowed:
		return "liberado"
	case sigadoc.Blocked:
		return "BLOQUEADO"
	case sigadoc.NotDeployed:
		return "não implantado"
	case sigadoc.Skipped:
		return "não sondado"
	default:
		return "sem resposta"
	}
}
