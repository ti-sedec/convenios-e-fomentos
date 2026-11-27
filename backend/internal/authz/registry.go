// Pacote authz é o motor de permissões: registro de ações e condições,
// avaliação (união entre roles, OR entre condições), filtro de campos, filtros
// seguros e cache com invalidação.
package authz

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Domain separa as ações por API. Hoje só existe a administrativa.
type Domain string

// Domínios de ações.
const (
	DomainAdmin Domain = "admin"
)

// Category agrupa ações na futura tela de permissões.
type Category string

// Categorias de ações.
const (
	CategoryCollectionTypes Category = "collection-types"
	CategorySingleTypes     Category = "single-types"
	CategoryPlugins         Category = "plugins"
	CategorySettings        Category = "settings"
	CategoryApplication     Category = "application"
)

// Action é uma ação protegida, registrada em código. Identificador no formato
// `dominio::recurso.acao`, por exemplo `admin::content.document.read`.
type Action struct {
	ID       string
	Domain   Domain
	Category Category
	Name     string // nome legível, em português
	Resource string // recurso associado (subject), quando houver

	SupportsFields     bool
	SupportsConditions bool
	SupportsLocales    bool // reservado para i18n futuro
}

var actionIDRe = regexp.MustCompile(`^admin::[a-z][a-z0-9-]*(\.[a-zA-Z][a-zA-Z0-9-]*)+$`)

// Registry é o registro central de ações (Action Registry). É preenchido na
// inicialização e somente lido depois, então não precisa de lock.
type Registry struct {
	actions map[string]Action
	order   []string
	schemas map[string]*Schema
}

// NewRegistry cria um registro vazio.
func NewRegistry() *Registry { return &Registry{actions: map[string]Action{}} }

// Register valida e adiciona uma ação.
func (r *Registry) Register(a Action) error {
	if !actionIDRe.MatchString(a.ID) {
		return fmt.Errorf("ação %q: identificador fora do formato dominio::recurso.acao", a.ID)
	}
	if prefix, _, _ := strings.Cut(a.ID, "::"); prefix != string(a.Domain) {
		return fmt.Errorf("ação %q: domínio %q não corresponde ao prefixo", a.ID, a.Domain)
	}
	if a.Name == "" {
		return fmt.Errorf("ação %q: nome legível é obrigatório", a.ID)
	}
	if a.Category == "" {
		return fmt.Errorf("ação %q: categoria é obrigatória", a.ID)
	}
	if _, dup := r.actions[a.ID]; dup {
		return fmt.Errorf("ação %q já registrada", a.ID)
	}
	r.actions[a.ID] = a
	r.order = append(r.order, a.ID)
	return nil
}

// MustRegister registra ou entra em pânico; para uso na inicialização.
func (r *Registry) MustRegister(actions ...Action) {
	for _, a := range actions {
		if err := r.Register(a); err != nil {
			panic(err)
		}
	}
}

// Get busca uma ação.
func (r *Registry) Get(id string) (Action, bool) { a, ok := r.actions[id]; return a, ok }

// All devolve as ações na ordem de registro.
func (r *Registry) All() []Action {
	out := make([]Action, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.actions[id])
	}
	return out
}

// IDs devolve os identificadores das ações do domínio.
func (r *Registry) IDs(d Domain) []string {
	var out []string
	for _, id := range r.order {
		if r.actions[id].Domain == d {
			out = append(out, id)
		}
	}
	return out
}

// Group é um conjunto de ações de uma categoria.
type Group struct {
	Category Category `json:"category"`
	Actions  []Action `json:"actions"`
}

var categoryOrder = []Category{CategoryCollectionTypes, CategorySingleTypes, CategoryPlugins, CategorySettings, CategoryApplication}

// ByCategory agrupa as ações do domínio por categoria, em ordem estável.
func (r *Registry) ByCategory(d Domain) []Group {
	var groups []Group
	for _, c := range categoryOrder {
		var list []Action
		for _, id := range r.order {
			if a := r.actions[id]; a.Domain == d && a.Category == c {
				list = append(list, a)
			}
		}
		if len(list) > 0 {
			groups = append(groups, Group{Category: c, Actions: list})
		}
	}
	return groups
}

// Resources lista os recursos distintos do domínio.
func (r *Registry) Resources(d Domain) []string {
	var out []string
	for _, id := range r.order {
		if a := r.actions[id]; a.Domain == d && a.Resource != "" && !slices.Contains(out, a.Resource) {
			out = append(out, a.Resource)
		}
	}
	return out
}
