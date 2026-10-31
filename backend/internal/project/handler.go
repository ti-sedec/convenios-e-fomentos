package project

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

	"github.com/wagner/convenios-e-fomentos/backend/internal/admin"
	"github.com/wagner/convenios-e-fomentos/backend/internal/auth"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
)

// Limits são os limites de requisições por minuto, por usuário, das rotas mais caras. Zero desliga o limite.
type Limits struct {
	// Upload vale para o pedido e a confirmação de envio de arquivo (a confirmação lê o arquivo inteiro).
	Upload int
	// Heavy vale para envio, reenvio, duplicação e comprovante.
	Heavy int
}

// DefaultHeavyLimit é o limite padrão das rotas pesadas: uma pessoa não envia nem duplica mais de 20 vezes por minuto.
const DefaultHeavyLimit = 20

// Handler expõe /admin/projects e /admin/proponent-profile.
type Handler struct {
	svc    *Service
	guard  func(action string) func(http.Handler) http.Handler
	limits Limits
}

// NewHandler cria o handler; guard autentica o administrador e exige a permissão.
func NewHandler(svc *Service, guard func(action string) func(http.Handler) http.Handler, limits Limits) *Handler {
	return &Handler{svc: svc, guard: guard, limits: limits}
}

// perUser limita as requisições por minuto de cada administrador (depois da autenticação). n <= 0 desliga.
func perUser(n int) func(http.Handler) http.Handler {
	if n <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return httprate.LimitBy(n, time.Minute,
		func(r *http.Request) (string, error) {
			p, _ := auth.AdminFrom(r.Context())
			if p == nil {
				return httpapi.ClientIP(r.Context()), nil
			}
			return p.ID.String(), nil
		},
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) { httpapi.Error(w, r, httpapi.ErrTooManyRequests()) }),
	)
}

// Routes registra as rotas.
func (h *Handler) Routes(r chi.Router) {
	g := h.guard
	r.With(g(ActionProfileRead)).Get("/admin/proponent-profile", h.getProfile)
	r.With(g(ActionProfileUpdate)).Put("/admin/proponent-profile", h.putProfile)

	r.With(g(ActionRead)).Get("/admin/projects", h.list)
	r.With(g(ActionRead)).Get("/admin/projects/overview", h.overview)
	r.With(g(ActionRead)).Get("/admin/projects/activity", h.activity)
	r.With(g(ActionStartAnalysis)).Get("/admin/projects/queue/overview", h.queueOverview)
	r.With(g(ActionStartAnalysis)).Get("/admin/projects/queue/activity", h.queueActivity)
	r.With(g(ActionCreate)).Post("/admin/projects", h.create)
	r.With(g(ActionRead)).Get("/admin/projects/{id}", h.get)
	r.With(g(ActionDelete)).Delete("/admin/projects/{id}", h.delete)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/proponent", h.putProponent)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/institution", h.putInstitution)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/proposal", h.putProposal)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/funding", h.putFunding)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/document-options", h.putDocumentOptions)

	heavy, upload := perUser(h.limits.Heavy), perUser(h.limits.Upload)
	r.With(g(ActionSubmit), heavy).Post("/admin/projects/{id}/submit", h.submit)
	r.With(g(ActionSubmit), heavy).Post("/admin/projects/{id}/resubmit", h.resubmit)
	r.With(g(ActionWithdraw)).Post("/admin/projects/{id}/withdraw", h.withdraw)
	r.With(g(ActionRequestComplement)).Post("/admin/projects/{id}/complement-requests", h.requestComplement)
	r.With(g(ActionArchive)).Post("/admin/projects/{id}/archive", h.archive)
	r.With(g(ActionStartAnalysis)).Post("/admin/projects/{id}/start-analysis", h.startAnalysis)
	r.With(g(ActionReviewItem)).Put("/admin/projects/{id}/review/items", h.reviewItem)
	r.With(g(ActionAssignDepartment)).Put("/admin/projects/{id}/department", h.assignDepartment)
	r.With(g(ActionReviewFraming)).Post("/admin/projects/{id}/framing/confirm", h.confirmFraming)
	r.With(g(ActionReviewFraming)).Post("/admin/projects/{id}/framing/refuse", h.refuseFraming)
	r.With(g(ActionDeny)).Post("/admin/projects/{id}/deny", h.deny)
	r.With(g(ActionCreate), heavy).Post("/admin/projects/{id}/duplicate", h.duplicate)
	r.With(g(ActionRead), heavy).Get("/admin/projects/{id}/receipt", h.receipt)

	r.With(g(ActionShare)).Get("/admin/projects/{id}/members", h.members)
	r.With(g(ActionShare), heavy).Post("/admin/projects/{id}/members", h.invite)
	r.With(g(ActionShare)).Put("/admin/projects/{id}/members/{memberId}", h.updateMember)
	r.With(g(ActionShare)).Delete("/admin/projects/{id}/members/{memberId}", h.revokeMember)

	r.With(g(ActionRead)).Get("/admin/projects/{id}/documents", h.checklist)
	r.With(g(ActionRead)).Get("/admin/projects/{id}/history", h.history)
	r.With(g(ActionRead)).Get("/admin/projects/{id}/declarations/{code}", h.declarationPreview)
	r.With(g(ActionUpdate), heavy).Post("/admin/projects/{id}/declarations/{code}/accept", h.acceptDeclaration)
	r.With(g(ActionUpdate)).Delete("/admin/projects/{id}/declarations/{code}", h.cancelDeclaration)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/documents/{code}/justification", h.putJustification)
	r.With(g(ActionUpdate), upload).Post("/admin/projects/{id}/documents/{code}/files", h.requestUpload)
	r.With(g(ActionUpdate)).Post("/admin/projects/{id}/documents/extra", h.createExtra)
	r.With(g(ActionUpdate)).Put("/admin/projects/{id}/documents/extra/{itemId}", h.updateExtra)
	r.With(g(ActionUpdate)).Delete("/admin/projects/{id}/documents/extra/{itemId}", h.deleteExtra)
	r.With(g(ActionUpdate), upload).Post("/admin/projects/{id}/documents/extra/{itemId}/files", h.requestExtraUpload)
	r.With(g(ActionUpdate), upload).Post("/admin/projects/{id}/documents/files/{fileId}/confirm", h.confirmUpload)
	r.With(g(ActionRead)).Get("/admin/projects/{id}/documents/files/{fileId}/download", h.download)
	r.With(g(ActionRead)).Get("/admin/projects/{id}/documents/files/{fileId}/preview", h.preview)
	r.With(g(ActionRead)).Post("/admin/projects/{id}/documents/files/{fileId}/viewed", h.viewed)
	r.With(g(ActionRead)).Post("/admin/projects/{id}/documents/files/{fileId}/read", h.readToEnd)
	r.With(g(ActionUpdate)).Delete("/admin/projects/{id}/documents/files/{fileId}", h.deleteFile)
}

func actorOf(r *http.Request) Actor {
	p, _ := auth.AdminFrom(r.Context())
	return Actor{ID: p.ID, CPF: p.CPF, Name: strings.TrimSpace(p.FirstName + " " + p.LastName), IP: httpapi.ClientIP(r.Context())}
}

func idParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, httpapi.ErrNotFound()
	}
	return id, nil
}

func (h *Handler) getProfile(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.GetProfile(r.Context(), actorOf(r))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) putProfile(w http.ResponseWriter, r *http.Request) {
	var in Contact
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	v, err := h.svc.SaveProfile(r.Context(), actorOf(r), in)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// parseStaffFilters lê os filtros das filas da equipe e recusa valor inválido com o nome do campo.
func parseStaffFilters(q url.Values, p *ListParams) error {
	var bad []httpapi.FieldError
	if v := q.Get("stage"); v != "" {
		for _, st := range strings.Split(v, ",") {
			switch st = strings.TrimSpace(st); st {
			case StageAwaitingChecklist, StageInAnalysis, StageAwaitingProponent, StageAwaitingOpinion:
				p.Stages = append(p.Stages, st)
			default:
				bad = append(bad, httpapi.FieldError{Field: "stage", Message: "Valores aceitos: awaiting_checklist, in_analysis, awaiting_proponent, awaiting_opinion."})
			}
		}
	}
	switch v := q.Get("framing"); v {
	case "", FramingPending, FramingConfirmed, "none":
		p.Framing = v
	default:
		bad = append(bad, httpapi.FieldError{Field: "framing", Message: "Valores aceitos: pending, confirmed, none."})
	}
	if v := q.Get("idleDays"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3650 {
			bad = append(bad, httpapi.FieldError{Field: "idleDays", Message: "Informe um número de dias entre 1 e 3650."})
		} else {
			p.IdleDays = n
		}
	}
	for _, f := range []struct {
		name string
		dst  **uuid.UUID
	}{{"department", &p.Department}, {"analyst", &p.Analyst}} {
		if v := q.Get(f.name); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				bad = append(bad, httpapi.FieldError{Field: f.name, Message: "Informe um identificador válido."})
				continue
			}
			*f.dst = &id
		}
	}
	if len(bad) > 0 {
		return httpapi.ErrValidation(map[string]any{"fields": bad})
	}
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	read, _ := admin.DecisionFrom(r.Context())
	page, err := httpapi.ParsePage(r, 25, 100)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	sort, err := httpapi.ParseSort(r, SortColumns, httpapi.Sort{Column: "updated_at", Desc: true})
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	q := r.URL.Query()
	p := ListParams{Perm: read.Filter, Actor: actorOf(r), Q: q.Get("q"), Status: q.Get("status"), Sort: sort, Page: page}
	if p.Status != "" && p.Status != StatusDraft && p.Status != StatusSubmitted && p.Status != StatusArchived {
		httpapi.Error(w, r, httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "status", Message: "Valores aceitos: draft, submitted, archived."}}}))
		return
	}
	if p.Access = q.Get("access"); p.Access != "" && !validAccess(p.Access) {
		httpapi.Error(w, r, errAccess())
		return
	}
	if err := parseStaffFilters(q, &p); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	list, total, err := h.svc.List(r.Context(), p)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, list, page.Meta(total))
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	read, _ := admin.DecisionFrom(r.Context())
	access := r.URL.Query().Get("access")
	if access != "" && !validAccess(access) {
		httpapi.Error(w, r, errAccess())
		return
	}
	v, err := h.svc.Overview(r.Context(), actorOf(r), read.Filter, access)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	read, _ := admin.DecisionFrom(r.Context())
	q := r.URL.Query()
	access := q.Get("access")
	if access != "" && !validAccess(access) {
		httpapi.Error(w, r, errAccess())
		return
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxActivityLimit {
			httpapi.Error(w, r, httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "limit", Message: "Informe um número entre 1 e " + strconv.Itoa(maxActivityLimit) + "."}}}))
			return
		}
		limit = n
	}
	v, err := h.svc.Activity(r.Context(), actorOf(r), read.Filter, access, limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) queueOverview(w http.ResponseWriter, r *http.Request) {
	dec, _ := admin.DecisionFrom(r.Context())
	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != QueueScopeTeam && scope != QueueScopeMine {
		httpapi.Error(w, r, httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "scope", Message: "Valores aceitos: team, mine."}}}))
		return
	}
	v, err := h.svc.QueueOverview(r.Context(), actorOf(r), dec.Filter, scope)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) queueActivity(w http.ResponseWriter, r *http.Request) {
	dec, _ := admin.DecisionFrom(r.Context())
	q := r.URL.Query()
	scope := q.Get("scope")
	if scope != "" && scope != QueueScopeTeam && scope != QueueScopeMine {
		httpapi.Error(w, r, httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "scope", Message: "Valores aceitos: team, mine."}}}))
		return
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxQueueActivityLimit {
			httpapi.Error(w, r, httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "limit", Message: "Informe um número entre 1 e " + strconv.Itoa(maxQueueActivityLimit) + "."}}}))
			return
		}
		limit = n
	}
	v, err := h.svc.QueueActivity(r.Context(), actorOf(r), dec.Filter, scope, limit)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func validAccess(a string) bool {
	return a == AccessFilterOwner || a == AccessFilterMember || a == AccessFilterAny
}

func errAccess() error {
	return httpapi.ErrValidation(map[string]any{"fields": []httpapi.FieldError{{Field: "access", Message: "Valores aceitos: all, owner, member."}}})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Create(r.Context(), actorOf(r))
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, v, nil)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.Get(r.Context(), actorOf(r), id, read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	del, _ := admin.DecisionFrom(r.Context())
	if err := h.svc.Delete(r.Context(), actorOf(r), id, del); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Cada seção é gravada inteira, junto com a versão que o cliente leu.
type proponentBody struct {
	Version int32 `json:"version" validate:"required,min=1"`
	ProponentSection
}

type institutionBody struct {
	Version int32 `json:"version" validate:"required,min=1"`
	InstitutionSection
}

type proposalBody struct {
	Version int32 `json:"version" validate:"required,min=1"`
	ProposalSection
}

func (h *Handler) putProponent(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in proponentBody
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.SaveProponent(r.Context(), actorOf(r), id, in.Version, in.ProponentSection, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) putInstitution(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in institutionBody
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.SaveInstitution(r.Context(), actorOf(r), id, in.Version, in.InstitutionSection, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) putProposal(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in proposalBody
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.SaveProposal(r.Context(), actorOf(r), id, in.Version, in.ProposalSection, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

type fundingBody struct {
	Version int32 `json:"version" validate:"required,min=1"`
	FundingSection
}

func (h *Handler) putFunding(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in fundingBody
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.SaveFunding(r.Context(), actorOf(r), id, in.Version, in.FundingSection, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

type documentOptionsBody struct {
	Version int32 `json:"version" validate:"required,min=1"`
	DocumentOptions
}

func (h *Handler) putDocumentOptions(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in documentOptionsBody
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.SaveDocumentOptions(r.Context(), actorOf(r), id, in.Version, in.DocumentOptions, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) checklist(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.Checklist(r.Context(), actorOf(r), id, read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.History(r.Context(), actorOf(r), id, read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

type justificationBody struct {
	AmendmentID string `json:"amendmentId"`
	Text        string `json:"text"`
}

func (h *Handler) putJustification(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in justificationBody
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.SetJustification(r.Context(), actorOf(r), id, chi.URLParam(r, "code"), in.AmendmentID, in.Text, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) requestUpload(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in FileInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	file, up, err := h.svc.RequestUpload(r.Context(), actorOf(r), id, chi.URLParam(r, "code"), in, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, map[string]any{"file": file, "upload": up}, nil)
}

func itemIDParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "itemId"))
	if err != nil {
		return uuid.Nil, httpapi.ErrNotFound()
	}
	return id, nil
}

func (h *Handler) createExtra(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in ExtraInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.CreateExtraDocument(r.Context(), actorOf(r), id, in, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, v, nil)
}

func (h *Handler) updateExtra(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	itemID, err2 := itemIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	var in ExtraInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.UpdateExtraDocument(r.Context(), actorOf(r), id, itemID, in, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) deleteExtra(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	itemID, err2 := itemIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.DeleteExtraDocument(r.Context(), actorOf(r), id, itemID, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) requestExtraUpload(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	itemID, err2 := itemIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	var in FileInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	file, up, err := h.svc.RequestExtraUpload(r.Context(), actorOf(r), id, itemID, in, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, map[string]any{"file": file, "upload": up}, nil)
}

func fileIDParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		return uuid.Nil, httpapi.ErrNotFound()
	}
	return id, nil
}

func (h *Handler) confirmUpload(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	fileID, err2 := fileIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.ConfirmUpload(r.Context(), id, fileID, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	fileID, err2 := fileIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.Download(r.Context(), actorOf(r), id, fileID, read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// preview devolve a URL para exibir o arquivo no navegador (o Word vem convertido em PDF).
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	fileID, err2 := fileIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.Preview(r.Context(), actorOf(r), id, fileID, read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// viewed registra que o arquivo foi exibido para a pessoa da equipe (prova para reprovar o documento).
func (h *Handler) viewed(w http.ResponseWriter, r *http.Request) { h.markViewed(w, r, false) }

// readToEnd registra que a pessoa da equipe chegou ao fim do arquivo (prova para aprovar o documento).
func (h *Handler) readToEnd(w http.ResponseWriter, r *http.Request) { h.markViewed(w, r, true) }

func (h *Handler) markViewed(w http.ResponseWriter, r *http.Request, toEnd bool) {
	id, err := idParam(r)
	fileID, err2 := fileIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	if err := h.svc.MarkViewed(r.Context(), actorOf(r), id, fileID, read, toEnd); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	fileID, err2 := fileIDParam(r)
	if err != nil || err2 != nil {
		httpapi.Error(w, r, httpapi.ErrNotFound())
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	if err := h.svc.DeleteFile(r.Context(), id, fileID, upd); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) submit(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	var in SubmitInput
	if err := httpapi.Decode(r, &in); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	sub, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.Submit(r.Context(), actorOf(r), id, in, sub)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// declarationPreview devolve o texto da declaração como será emitido e as pendências de dados.
func (h *Handler) declarationPreview(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.DeclarationPreview(r.Context(), actorOf(r), id, chi.URLParam(r, "code"), read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// acceptDeclaration grava o aceite do texto lido, gera o PDF e pede a assinatura.
func (h *Handler) acceptDeclaration(w http.ResponseWriter, r *http.Request) {
	in, id, upd, ok := decode[AcceptDeclarationInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.AcceptDeclaration(r.Context(), actorOf(r), id, chi.URLParam(r, "code"), in, upd)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

// cancelDeclaration cancela o pedido de assinatura em andamento.
func (h *Handler) cancelDeclaration(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	upd, _ := admin.DecisionFrom(r.Context())
	if err := h.svc.CancelDeclaration(r.Context(), actorOf(r), id, chi.URLParam(r, "code"), upd); err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// receipt devolve o comprovante de recebimento em PDF (não é o envelope JSON padrão).
func (h *Handler) receipt(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	read, _ := admin.DecisionFrom(r.Context())
	pdf, name, err := h.svc.Receipt(r.Context(), actorOf(r), id, read)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf) //nolint:gosec // PDF gerado pelo servidor, servido como application/pdf (não é HTML)
}

// decode lê o corpo e devolve a decisão de permissão da rota; num único passo para os handlers do ciclo de vida.
func decode[T any](w http.ResponseWriter, r *http.Request) (T, uuid.UUID, authz.Decision, bool) {
	var in T
	id, err := idParam(r)
	if err == nil {
		err = httpapi.Decode(r, &in)
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return in, uuid.Nil, authz.Decision{}, false
	}
	dec, _ := admin.DecisionFrom(r.Context())
	return in, id, dec, true
}

func respond(w http.ResponseWriter, r *http.Request, v *Project, err error) {
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, v, nil)
}

func (h *Handler) resubmit(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[SubmitInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.Resubmit(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

type noteBody struct {
	Note string `json:"note"`
}

func (h *Handler) withdraw(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[noteBody](w, r)
	if !ok {
		return
	}
	v, err := h.svc.Withdraw(r.Context(), actorOf(r), id, in.Note, dec)
	respond(w, r, v, err)
}

func (h *Handler) deny(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[noteBody](w, r)
	if !ok {
		return
	}
	v, err := h.svc.Deny(r.Context(), actorOf(r), id, in.Note, dec)
	respond(w, r, v, err)
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[ArchiveInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.Archive(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

func (h *Handler) requestComplement(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[ComplementInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.RequestComplement(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

func (h *Handler) startAnalysis(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	dec, _ := admin.DecisionFrom(r.Context())
	v, err := h.svc.StartAnalysis(r.Context(), actorOf(r), id, dec)
	respond(w, r, v, err)
}

func (h *Handler) reviewItem(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[ReviewInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.ReviewItem(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

func (h *Handler) assignDepartment(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[DepartmentInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.AssignDepartment(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

func (h *Handler) confirmFraming(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[FramingDecisionInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.ConfirmFraming(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

func (h *Handler) refuseFraming(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[FramingDecisionInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.RefuseFraming(r.Context(), actorOf(r), id, in, dec)
	respond(w, r, v, err)
}

func (h *Handler) duplicate(w http.ResponseWriter, r *http.Request) {
	in, id, _, ok := decode[DuplicateInput](w, r)
	if !ok {
		return
	}
	v, err := h.svc.Duplicate(r.Context(), actorOf(r), id, in)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, v, nil)
}

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	dec, _ := admin.DecisionFrom(r.Context())
	list, err := h.svc.Members(r.Context(), id, dec)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, list, nil)
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[MemberInput](w, r)
	if !ok {
		return
	}
	m, err := h.svc.Invite(r.Context(), actorOf(r), id, in, dec)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, m, nil)
}

func memberParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		return uuid.Nil, httpapi.ErrNotFound()
	}
	return id, nil
}

func (h *Handler) updateMember(w http.ResponseWriter, r *http.Request) {
	in, id, dec, ok := decode[MemberInput](w, r)
	if !ok {
		return
	}
	memberID, err := memberParam(r)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	m, err := h.svc.UpdateMember(r.Context(), actorOf(r), id, memberID, in, dec)
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, m, nil)
}

func (h *Handler) revokeMember(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err == nil {
		var memberID uuid.UUID
		if memberID, err = memberParam(r); err == nil {
			dec, _ := admin.DecisionFrom(r.Context())
			err = h.svc.RevokeMember(r.Context(), actorOf(r), id, memberID, dec)
		}
	}
	if err != nil {
		httpapi.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
