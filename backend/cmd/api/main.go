// Comando api sobe o servidor HTTP.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wagner/convenios-e-fomentos/backend/internal/app"
	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/logging"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mailer"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mtid"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateOIDC(); err != nil {
		return err
	}
	log := logging.New(os.Stdout, cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var mail mailer.Mailer = mailer.NopMailer{Log: log}
	switch {
	case cfg.SMTPHost != "":
		mail = mailer.SMTP{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.SMTPFrom}
	case !cfg.IsProduction():
		mail = mailer.LogMailer{Log: log}
	}
	var opts []app.Option
	if cfg.MTIDSimulate {
		sim := mtid.NewMemory()
		sim.AutoApprove = cfg.MTIDSimulateDelay
		opts = append(opts, app.WithMTID(sim))
		log.Warn("MT-ID SIMULADO ligado: as declarações são \"assinadas\" sozinhas, sem validade", "delay", cfg.MTIDSimulateDelay)
	} else if cfg.XViaBaseURL != "" {
		real, err := mtid.NewHTTP(mtid.HTTPConfig{
			BaseURL: cfg.XViaBaseURL, Service: cfg.XViaMTIDService, XRoadClient: cfg.XRoadClient,
			CAFile: cfg.XViaCAFile, TLSServerName: cfg.XViaTLSServerName, Timeout: cfg.XViaTimeout, Log: log,
		})
		if err != nil {
			return err
		}
		opts = append(opts, app.WithMTID(real))
		log.Info("MT-ID real ligado", "baseURL", cfg.XViaBaseURL, "xRoadClient", cfg.XRoadClient)
	}
	application, err := app.New(ctx, cfg, log, pool, mail, opts...)
	if err != nil {
		return err
	}
	if err := application.Start(ctx); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           application.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("servidor iniciado", "addr", srv.Addr, "env", cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("encerrando servidor")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	application.APITokens.Wait() // conclui as gravações pendentes de last_used_at
	if !application.Wait(20 * time.Second) {
		log.Warn("o envio de e-mails não terminou a tempo; as mensagens em andamento serão retomadas por outra instância")
	}
	return err
}
