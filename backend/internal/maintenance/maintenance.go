// Pacote maintenance reúne rotinas de limpeza de dados que só crescem (e-mails já resolvidos).
package maintenance

import (
	"context"
	"fmt"
	"time"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// Result informa quantas linhas cada rotina removeu.
type Result struct {
	// ResolvedEmails são os e-mails da caixa de saída já enviados (ou desistidos). O corpo tem dados pessoais, então não se guarda.
	ResolvedEmails int64
}

// Cleanup remove os e-mails resolvidos há mais de graceDays dias.
// É seguro executar a qualquer momento e quantas vezes for preciso (idempotente).
func Cleanup(ctx context.Context, q *db.Queries, graceDays int) (Result, error) {
	if graceDays < 0 {
		return Result{}, fmt.Errorf("graceDays deve ser >= 0")
	}
	var r Result
	var err error
	if r.ResolvedEmails, err = q.PurgeResolvedEmails(ctx, time.Now().AddDate(0, 0, -graceDays)); err != nil {
		return r, fmt.Errorf("e-mails já enviados: %w", err)
	}
	return r, nil
}
