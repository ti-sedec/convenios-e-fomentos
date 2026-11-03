package rules

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// Parameter é uma versão de um parâmetro, com sua vigência.
type Parameter struct {
	ID        uuid.UUID       `json:"id"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	ValidFrom string          `json:"validFrom"`
	ValidTo   *string         `json:"validTo"`
	Note      string          `json:"note"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// KeyInfo descreve uma chave conhecida, para a tela de administração.
type KeyInfo struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// Input é o corpo de criação e alteração.
type Input struct {
	Key       string          `json:"key" validate:"omitempty"`
	Value     json.RawMessage `json:"value" validate:"required"`
	ValidFrom string          `json:"validFrom" validate:"required"`
	ValidTo   *string         `json:"validTo"`
	Note      string          `json:"note" validate:"max=500"`
}

// Actor é o administrador que executa a operação, para a auditoria.
type Actor struct {
	ID uuid.UUID
	IP string
}

func (a Actor) entry(action string, id uuid.UUID, before, after any) audit.Entry {
	payload := map[string]any{}
	if before != nil {
		payload["before"] = before
	}
	if after != nil {
		payload["after"] = after
	}
	return audit.Entry{ActorType: "admin", ActorID: a.ID.String(), Action: action, Entity: "rule_parameter", EntityID: id.String(), Payload: payload, IP: a.IP}
}

func fieldError(field, msg string) error {
	return httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: field, Message: msg}}})
}

// Service implementa as operações sobre os parâmetros.
type Service struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	audit audit.Recorder
	now   func() time.Time
}

// NewService cria o serviço.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: db.New(pool), now: time.Now}
}

// Keys devolve as chaves conhecidas.
func (s *Service) Keys() []KeyInfo {
	specs := Specs()
	out := make([]KeyInfo, len(specs))
	for i, sp := range specs {
		out[i] = KeyInfo{Key: sp.Key, Description: sp.Description, Source: sp.Source}
	}
	return out
}

func view(p db.RuleParameter) Parameter {
	out := Parameter{ID: p.ID, Key: p.Key, Value: p.Value, ValidFrom: p.ValidFrom.Time.Format(time.DateOnly), Note: p.Note, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	if p.ValidTo.Valid {
		v := p.ValidTo.Time.Format(time.DateOnly)
		out.ValidTo = &v
	}
	return out
}

// List devolve as versões (de uma chave, se informada), da mais recente para a mais antiga.
func (s *Service) List(ctx context.Context, key string) ([]Parameter, error) {
	var k *string
	if key != "" {
		k = &key
	}
	rows, err := s.q.ListRuleParameters(ctx, k)
	if err != nil {
		return nil, err
	}
	out := make([]Parameter, len(rows))
	for i, r := range rows {
		out[i] = view(r)
	}
	return out, nil
}

// Get devolve uma versão.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Parameter, error) {
	r, err := s.q.GetRuleParameter(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	v := view(r)
	return &v, nil
}

// Effective devolve, para cada chave, a versão vigente na data (chaves sem versão vigente ficam de fora).
func (s *Service) Effective(ctx context.Context, date time.Time) ([]Parameter, error) {
	return effective(ctx, s.q, date)
}

func effective(ctx context.Context, q *db.Queries, date time.Time) ([]Parameter, error) {
	rows, err := q.ListEffectiveRuleParameters(ctx, pgtype.Date{Time: date, Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]Parameter, len(rows))
	for i, r := range rows {
		out[i] = view(r)
	}
	return out, nil
}

// Today é a data de hoje no fuso de Cuiabá, usada como padrão das consultas de vigência.
func (s *Service) Today() time.Time {
	loc, err := time.LoadLocation("America/Cuiaba")
	if err != nil {
		loc = time.FixedZone("-04", -4*3600)
	}
	n := s.now().In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// parsed é a entrada já validada.
type parsed struct {
	value     json.RawMessage
	validFrom pgtype.Date
	validTo   pgtype.Date
	note      string
}

func (s *Service) parse(key string, in Input) (*parsed, error) {
	spec, ok := SpecFor(key)
	if !ok {
		return nil, fieldError("key", "Chave de parâmetro desconhecida.")
	}
	if err := spec.Validate(in.Value); err != nil {
		return nil, fieldError("value", err.Error())
	}
	from, err := time.Parse(time.DateOnly, in.ValidFrom)
	if err != nil {
		return nil, fieldError("validFrom", "Informe uma data no formato AAAA-MM-DD.")
	}
	p := &parsed{value: in.Value, validFrom: pgtype.Date{Time: from, Valid: true}, note: in.Note}
	if in.ValidTo != nil && *in.ValidTo != "" {
		to, err := time.Parse(time.DateOnly, *in.ValidTo)
		if err != nil {
			return nil, fieldError("validTo", "Informe uma data no formato AAAA-MM-DD.")
		}
		if to.Before(from) {
			return nil, fieldError("validTo", "O fim da vigência não pode ser anterior ao início.")
		}
		p.validTo = pgtype.Date{Time: to, Valid: true}
	}
	return p, nil
}

func errOverlap() error {
	return httpapi.ErrConflict("rule_parameter_overlap", "Já existe uma versão desta chave com vigência que se sobrepõe a este período.")
}

func errVersionExists() error {
	return httpapi.ErrConflict("rule_parameter_exists", "Já existe uma versão desta chave que começa nesta data.")
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpapi.ErrNotFound()
	}
	return err
}

// checkOverlap recusa períodos que se sobrepõem a outra versão da mesma chave.
func (s *Service) checkOverlap(ctx context.Context, q *db.Queries, key string, id uuid.UUID, p *parsed) error {
	n, err := q.CountOverlappingRuleParameters(ctx, db.CountOverlappingRuleParametersParams{Key: key, ID: id, ValidTo: p.validTo, ValidFrom: p.validFrom})
	if err != nil {
		return err
	}
	if n > 0 {
		return errOverlap()
	}
	return nil
}

// Create cria uma versão.
func (s *Service) Create(ctx context.Context, actor Actor, in Input) (*Parameter, error) {
	p, err := s.parse(in.Key, in)
	if err != nil {
		return nil, err
	}
	var out Parameter
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockRuleParameterKey(ctx, in.Key); err != nil {
			return err
		}
		if err := s.checkOverlap(ctx, q, in.Key, uuid.Nil, p); err != nil {
			return err
		}
		row, err := q.CreateRuleParameter(ctx, db.CreateRuleParameterParams{Key: in.Key, Value: p.value, ValidFrom: p.validFrom, ValidTo: p.validTo, Note: p.note})
		if database.IsUniqueViolation(err) {
			return errVersionExists()
		}
		if err != nil {
			return err
		}
		out = view(row)
		return s.audit.Record(ctx, q, actor.entry("rule-parameter.create", row.ID, nil, out))
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Update altera valor, vigência e nota de uma versão. A chave não muda.
func (s *Service) Update(ctx context.Context, actor Actor, id uuid.UUID, in Input) (*Parameter, error) {
	cur, err := s.q.GetRuleParameter(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if in.Key != "" && in.Key != cur.Key {
		return nil, fieldError("key", "A chave de uma versão não pode ser alterada.")
	}
	p, err := s.parse(cur.Key, in)
	if err != nil {
		return nil, err
	}
	var out Parameter
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockRuleParameterKey(ctx, cur.Key); err != nil {
			return err
		}
		before, err := q.GetRuleParameterForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if err := s.checkOverlap(ctx, q, cur.Key, id, p); err != nil {
			return err
		}
		row, err := q.UpdateRuleParameter(ctx, db.UpdateRuleParameterParams{ID: id, Value: p.value, ValidFrom: p.validFrom, ValidTo: p.validTo, Note: p.note})
		if database.IsUniqueViolation(err) {
			return errVersionExists()
		}
		if err != nil {
			return err
		}
		out = view(row)
		return s.audit.Record(ctx, q, actor.entry("rule-parameter.update", id, view(before), out))
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete exclui uma versão.
func (s *Service) Delete(ctx context.Context, actor Actor, id uuid.UUID) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		before, err := q.GetRuleParameterForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		if _, err := q.DeleteRuleParameter(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, q, actor.entry("rule-parameter.delete", id, view(before), nil))
	})
}

// EnsureDefaults cria os valores iniciais das chaves que ainda não têm nenhuma versão. Não altera
// o que um administrador já cadastrou nem recria versões excluídas de chaves que já têm outras.
func (s *Service) EnsureDefaults(ctx context.Context) error {
	for _, sp := range Specs() {
		for _, d := range sp.Defaults {
			from, err := time.Parse(time.DateOnly, d.ValidFrom)
			if err != nil {
				return err
			}
			p := db.InsertDefaultRuleParameterParams{Key: sp.Key, Value: []byte(d.Value), ValidFrom: pgtype.Date{Time: from, Valid: true}, Note: d.Note}
			if d.ValidTo != "" {
				to, err := time.Parse(time.DateOnly, d.ValidTo)
				if err != nil {
					return err
				}
				p.ValidTo = pgtype.Date{Time: to, Valid: true}
			}
			if _, err := s.q.InsertDefaultRuleParameter(ctx, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// Snapshot são os parâmetros vigentes numa data, já interpretados. Chave sem versão vigente fica nula/zerada.
type Snapshot struct {
	Lei12082        *Lei12082          `json:"lei12082,omitempty"`
	IndirectTiers   []IndirectCostTier `json:"indirectTiers,omitempty"`
	PriceMaxAgeDays int                `json:"priceMaxAgeDays"`          // 0 = sem regra vigente
	ComplementDays  int                `json:"complementDays"`           // 0 = sem regra vigente
	ElectoralLimit  string             `json:"electoralLimit,omitempty"` // AAAA-MM-DD; vazio = sem data-limite vigente
	StalledAfter    *StalledAfterDays  `json:"stalledAfter,omitempty"`   // nulo = sem regra vigente
}

// SnapshotAt interpreta os parâmetros vigentes na data. Valor guardado fora do formato esperado é ignorado
// (a validação na gravação impede que isso aconteça). Recebe o q do chamador para ler na mesma transação:
// pedir outra conexão ao pool com a transação aberta trava a API quando o pool está cheio de transações esperando um lock.
func (s *Service) SnapshotAt(ctx context.Context, q *db.Queries, date time.Time) (*Snapshot, error) {
	list, err := effective(ctx, q, date)
	if err != nil {
		return nil, err
	}
	var out Snapshot
	for _, p := range list {
		switch p.Key {
		case KeyLei12082:
			var v Lei12082
			if json.Unmarshal(p.Value, &v) == nil {
				out.Lei12082 = &v
			}
		case KeyIndirectCostTiers:
			_ = json.Unmarshal(p.Value, &out.IndirectTiers)
		case KeyPriceReferenceDays:
			_ = json.Unmarshal(p.Value, &out.PriceMaxAgeDays)
		case KeyComplementDays:
			_ = json.Unmarshal(p.Value, &out.ComplementDays)
		case KeyElectoralDeadline:
			_ = json.Unmarshal(p.Value, &out.ElectoralLimit)
		case KeyStalledAfterDays:
			var v StalledAfterDays
			if json.Unmarshal(p.Value, &v) == nil {
				out.StalledAfter = &v
			}
		}
	}
	return &out, nil
}
