package authz

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

// rule é uma permissão aplicável: registros que casam com o filtro podem ser
// acessados com os campos informados.
type rule struct {
	filter Filter
	fields FieldSet
}

// Decision é o resultado de Can.
//
// Allowed indica se há alguma permissão aplicável. Filter restringe os
// registros acessíveis (FilterTrue = sem restrição) e deve ser aplicado nas
// listagens. Fields são os campos permitidos: para ações de leitura, os
// legíveis; para create/update, os graváveis. Como permissões diferentes podem
// ter filtros e campos diferentes, FieldsFor calcula os campos de um registro
// específico (união das permissões cujo filtro casa com ele).
type Decision struct {
	Allowed bool
	Filter  Filter
	Fields  FieldSet
	rules   []rule
}

func denied() Decision { return Decision{Filter: FilterFalse()} }

// FieldsFor devolve os campos permitidos para um registro concreto.
func (d Decision) FieldsFor(get Getter) FieldSet {
	var out FieldSet
	for _, r := range d.rules {
		if r.filter.Match(get) {
			out = out.Union(r.fields)
		}
	}
	return out
}

// AllowsRecord informa se a decisão permite agir sobre o registro.
func (d Decision) AllowsRecord(get Getter) bool { return d.Allowed && d.Filter.Match(get) }

// Engine avalia permissões.
type Engine struct {
	actions *Registry
	conds   *ConditionRegistry
	cache   *cache // permissões das roles de administrador
	bus     Broadcaster
	log     *slog.Logger
}

// NewEngine cria o motor. bus propaga invalidações para outras instâncias;
// use LocalBroadcaster em instância única.
func NewEngine(actions *Registry, conds *ConditionRegistry, store Store, bus Broadcaster, log *slog.Logger) *Engine {
	return &Engine{actions: actions, conds: conds, cache: newCache(store), bus: bus, log: log}
}

// Actions expõe o registro de ações.
func (e *Engine) Actions() *Registry { return e.actions }

// Conditions expõe o registro de condições.
func (e *Engine) Conditions() *ConditionRegistry { return e.conds }

// Invalidate descarta imediatamente o cache local das roles (todas, se nenhuma
// for informada) e avisa as demais instâncias.
func (e *Engine) Invalidate(ctx context.Context, roleIDs ...uuid.UUID) error {
	e.invalidateLocal(roleIDs...)
	if e.bus == nil {
		return nil
	}
	return e.bus.Publish(ctx, roleIDs)
}

// Listen aplica ao cache local as invalidações vindas de outras instâncias.
// Bloqueia até o contexto terminar.
func (e *Engine) Listen(ctx context.Context) error {
	if e.bus == nil {
		return nil
	}
	return e.bus.Subscribe(ctx, func(ids []uuid.UUID) { e.invalidateLocal(ids...) })
}

// invalidateLocal limpa o cache de permissões desta instância.
func (e *Engine) invalidateLocal(roleIDs ...uuid.UUID) {
	e.cache.invalidate(roleIDs...)
}

// Can decide se o sujeito pode executar a ação. resource é opcional e, quando
// informado, restringe às permissões daquele recurso. Ação não registrada,
// sujeito sem permissão ou que não é administrador resultam em negação (sem erro).
func (e *Engine) Can(ctx context.Context, s Subject, action string, resource ...string) (Decision, error) {
	if _, ok := e.actions.Get(action); !ok {
		e.log.WarnContext(ctx, "verificação de ação não registrada; negando", "action", action)
		return denied(), nil
	}
	if s.Type != SubjectAdmin {
		return denied(), nil
	}
	if s.IsSuperAdmin() {
		return Decision{Allowed: true, Filter: FilterTrue(), Fields: AllFields(), rules: []rule{{FilterTrue(), AllFields()}}}, nil
	}

	var res string
	if len(resource) > 0 {
		res = resource[0]
	}
	var rules []rule
	for _, role := range s.Roles {
		perms, err := e.cache.get(ctx, role.ID)
		if err != nil {
			return Decision{}, fmt.Errorf("carregando permissões da role %s: %w", role.ID, err)
		}
		for _, p := range perms {
			if p.Action != action || (res != "" && p.Subject != res) {
				continue
			}
			f, err := e.permissionFilter(ctx, s, p)
			if err != nil {
				return Decision{}, err
			}
			if f.IsFalse() {
				continue
			}
			rules = append(rules, rule{filter: f, fields: fieldSetFromList(p.Fields)})
		}
	}
	if len(rules) == 0 {
		return denied(), nil
	}
	filters := make([]Filter, len(rules))
	var fields FieldSet
	for i, r := range rules {
		filters[i] = r.filter
		fields = fields.Union(r.fields)
	}
	return Decision{Allowed: true, Filter: Or(filters...), Fields: fields, rules: rules}, nil
}

// permissionFilter combina as condições da permissão com OR. Sem condições, a
// permissão é incondicional. Condição desconhecida conta como falsa (nega).
func (e *Engine) permissionFilter(ctx context.Context, s Subject, p Permission) (Filter, error) {
	if len(p.Conditions) == 0 {
		return FilterTrue(), nil
	}
	fs := make([]Filter, 0, len(p.Conditions))
	for _, id := range p.Conditions {
		c, ok := e.conds.Get(id)
		if !ok {
			e.log.WarnContext(ctx, "condição desconhecida em permissão; ignorando", "condition", id, "action", p.Action)
			continue
		}
		r, err := c.Handler(ctx, s)
		if err != nil {
			return FilterFalse(), fmt.Errorf("avaliando condição %s: %w", id, err)
		}
		fs = append(fs, r.filter)
	}
	return Or(fs...), nil
}
