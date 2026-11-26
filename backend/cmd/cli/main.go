// Comando cli reúne os comandos administrativos (migrações, administradores, etc.).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/wagner/convenios-e-fomentos/backend/internal/admin"
	"github.com/wagner/convenios-e-fomentos/backend/internal/app"
	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/logging"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mailer"
	"github.com/wagner/convenios-e-fomentos/backend/internal/maintenance"
	"github.com/wagner/convenios-e-fomentos/backend/internal/seed"
)

const uso = `Uso: cli <comando> [opções]

Comandos:
  migrate:up         aplica as migrações pendentes
  migrate:down       desfaz a última migração
  migrate:status     mostra o estado das migrações
  admin:create       cria (ou promove) um Super Admin pelo CPF; o login é pelo MT Login e
                     o nome e o e-mail são preenchidos no primeiro acesso
                     --cpf 000.000.000-00 [--first-name N] [--last-name S] [--email E]
  seed               cria os dados iniciais (idempotente): roles e permissões padrão,
                     departamentos de exemplo, os Super Admins de SEED_SUPER_ADMIN_CPF e,
                     em desenvolvimento, um administrador de exemplo por role
  sigadoc:check      autentica no SIGADOC pelo X-Via e mostra quais métodos o usuário de sistema
                     consegue chamar (não altera nada; sem banco)
                     --matricula SEDEC123456 [--strict]
  cleanup            remove sessões de usuário final e tokens de uso único vencidos há mais
                     de N dias [--days 7] e envios de arquivo nunca confirmados há mais de 24 h;
                     com --draft-days N, também apaga rascunhos de projeto sem alteração há mais
                     de N dias (retenção; padrão 0 = não apaga). Agende, por exemplo, uma vez por dia
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, uso)
		os.Exit(2)
	}
	if err := run(context.Background(), os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd string, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cmd == "sigadoc:check" { // não precisa do banco
		return sigadocCheck(ctx, cfg, args, os.Stdout)
	}
	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch cmd {
	case "migrate:up", "migrate:down", "migrate:status":
		m := database.NewMigrator(pool)
		defer func() { _ = m.Close() }()
		switch cmd {
		case "migrate:up":
			return m.Up(ctx)
		case "migrate:down":
			return m.Down(ctx)
		default:
			return m.Status(ctx)
		}
	case "admin:create":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		cpfFlag := fs.String("cpf", "", "CPF do administrador")
		first := fs.String("first-name", "", "nome (opcional; o MT Login preenche no primeiro acesso)")
		last := fs.String("last-name", "", "sobrenome (opcional)")
		email := fs.String("email", "", "e-mail (opcional)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *cpfFlag == "" {
			return errors.New("--cpf é obrigatório")
		}
		a, err := app.AdminService(pool).EnsureSuperAdmin(ctx, admin.EnsureSuperAdminInput{CPF: *cpfFlag, FirstName: *first, LastName: *last, Email: *email}, "")
		if err != nil {
			return err
		}
		fmt.Printf("Super Admin garantido: %s (%s)\n", cpf.Mask(*a.Cpf), a.ID)
		return nil
	case "seed":
		a, err := app.New(ctx, cfg, logging.New(os.Stdout, cfg.LogLevel), pool, mailer.NopMailer{Log: logging.New(os.Stdout, cfg.LogLevel)})
		if err != nil {
			return err
		}
		return seed.Run(ctx, seed.Deps{Pool: pool, Hasher: app.Hasher(cfg), Engine: a.Authz, EndUsers: a.EndUsers, Log: logging.New(os.Stdout, cfg.LogLevel), Development: cfg.AppEnv == "development", SuperAdminCPFs: cfg.SeedSuperAdminCPFs})
	case "cleanup":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		days := fs.Int("days", 7, "carência, em dias, após o vencimento")
		draftDays := fs.Int("draft-days", 0, "apaga rascunhos de projeto sem alteração há mais de N dias (0 = não apaga)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		res, err := maintenance.Cleanup(ctx, db.New(pool), *days)
		if err != nil {
			return err
		}
		fmt.Printf("Removidos: %d sessões de usuário final, %d tokens de uso único, %d e-mails já enviados.\n", res.EndUserSessions, res.OneTimeTokens, res.ResolvedEmails)
		// Arquivos com envio autorizado e nunca confirmado há mais de 24 horas.
		a, err := app.New(ctx, cfg, logging.New(os.Stdout, cfg.LogLevel), pool, mailer.NopMailer{Log: logging.New(os.Stdout, cfg.LogLevel)})
		if err != nil {
			return err
		}
		n, err := a.Documents.CleanupPendingFiles(ctx)
		if err != nil {
			return err
		}
		m, err := a.Projects.CleanupPendingFiles(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Removidos: %d envios de arquivo não confirmados (documentos) e %d (projetos).\n", n, m)
		drafts, err := a.Projects.CleanupAbandonedDrafts(ctx, *draftDays)
		if err != nil {
			return err
		}
		if *draftDays > 0 {
			fmt.Printf("Removidos: %d rascunhos de projeto sem alteração há mais de %d dias.\n", drafts, *draftDays)
		}
		return nil
	default:
		return fmt.Errorf("comando desconhecido %q\n\n%s", cmd, uso)
	}
}
