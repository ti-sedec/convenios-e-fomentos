// Pacote audit grava o registro de auditoria.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// Entry descreve um evento auditável. Payload costuma conter o estado anterior
// e o novo (por exemplo {"before": ..., "after": ...}).
type Entry struct {
	ActorType string // admin, end_user, api_token, system
	ActorID   string
	Action    string
	Entity    string
	EntityID  string
	Payload   any
	IP        string
}

// Recorder grava entradas usando as queries informadas (pool ou transação).
type Recorder struct{}

// Record insere a entrada. Passe as queries de uma transação para que a
// auditoria seja confirmada junto com a alteração.
func (r Recorder) Record(ctx context.Context, q *db.Queries, e Entry) error {
	_, err := r.RecordID(ctx, q, e)
	return err
}

// RecordID faz o mesmo que Record e devolve o id do registro criado, para quem precisa ligar outro dado ao evento.
func (Recorder) RecordID(ctx context.Context, q *db.Queries, e Entry) (uuid.UUID, error) {
	payload := []byte("{}")
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return uuid.Nil, fmt.Errorf("serializando payload de auditoria: %w", err)
		}
		payload = b
	}
	return q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ActorType: e.ActorType,
		ActorID:   text(e.ActorID),
		Action:    e.Action,
		Entity:    e.Entity,
		EntityID:  text(e.EntityID),
		Payload:   payload,
		Ip:        text(e.IP),
	})
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
