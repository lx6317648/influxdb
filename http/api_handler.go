package http

import (
	"io"
	http "net/http"
	"strings"

	"github.com/influxdata/platform"
	"github.com/influxdata/platform/chronograf/server"
	"github.com/influxdata/platform/query"
	pzap "github.com/influxdata/platform/zap"
	"go.uber.org/zap"
)

// APIHandler is a collection of all the service handlers.
type APIHandler struct {
	BucketHandler        *BucketHandler
	UserHandler          *UserHandler
	OrgHandler           *OrgHandler
	AuthorizationHandler *AuthorizationHandler
	DashboardHandler     *DashboardHandler
	AssetHandler         *AssetHandler
	ChronografHandler    *ChronografHandler
	ViewHandler          *ViewHandler
	SourceHandler        *SourceHandler
	MacroHandler         *MacroHandler
	TaskHandler          *TaskHandler
	FluxLangHandler      *FluxLangHandler
	QueryHandler         *FluxHandler
	WriteHandler         *WriteHandler
	SetupHandler         *SetupHandler
	SessionHandler       *SessionHandler
}

// APIBackend is all services and associated parameters required to construct
// an APIHandler.
type APIBackend struct {
	Logger *zap.Logger

	NewBucketService func(*platform.Source) (platform.BucketService, error)
	NewQueryService  func(*platform.Source) (query.ProxyQueryService, error)

	PublisherFn func(r io.Reader) error

	AuthorizationService       platform.AuthorizationService
	BucketService              platform.BucketService
	SessionService             platform.SessionService
	UserService                platform.UserService
	OrganizationService        platform.OrganizationService
	UserResourceMappingService platform.UserResourceMappingService
	DashboardService           platform.DashboardService
	ViewService                platform.ViewService
	SourceService              platform.SourceService
	MacroService               platform.MacroService
	BasicAuthService           platform.BasicAuthService
	OnboardingService          platform.OnboardingService
	QueryService               query.QueryService
	TaskService                platform.TaskService
	ScraperTargetStoreService  platform.ScraperTargetStoreService
	ChronografService          *server.Service
}

// NewAPIHandler constructs all api handlers beneath it and returns an APIHandler
func NewAPIHandler(b *APIBackend) *APIHandler {
	h := &APIHandler{}
	h.SessionHandler = NewSessionHandler()
	h.SessionHandler.BasicAuthService = b.BasicAuthService
	h.SessionHandler.SessionService = b.SessionService

	h.BucketHandler = NewBucketHandler()
	h.BucketHandler.BucketService = b.BucketService

	h.OrgHandler = NewOrgHandler()
	h.OrgHandler.OrganizationService = b.OrganizationService
	h.OrgHandler.BucketService = b.BucketService
	h.OrgHandler.UserResourceMappingService = b.UserResourceMappingService

	h.UserHandler = NewUserHandler()
	h.UserHandler.UserService = b.UserService

	h.DashboardHandler = NewDashboardHandler()
	h.DashboardHandler.DashboardService = b.DashboardService

	h.ViewHandler = NewViewHandler()
	h.ViewHandler.ViewService = b.ViewService

	h.MacroHandler = NewMacroHandler()
	h.MacroHandler.MacroService = b.MacroService

	h.AuthorizationHandler = NewAuthorizationHandler()
	h.AuthorizationHandler.AuthorizationService = b.AuthorizationService
	h.AuthorizationHandler.Logger = b.Logger.With(zap.String("handler", "auth"))

	h.FluxLangHandler = NewFluxLangHandler()

	h.SourceHandler = NewSourceHandler()
	h.SourceHandler.SourceService = b.SourceService
	h.SourceHandler.NewBucketService = b.NewBucketService
	h.SourceHandler.NewQueryService = b.NewQueryService

	h.SetupHandler = NewSetupHandler()
	h.SetupHandler.OnboardingService = b.OnboardingService

	h.TaskHandler = NewTaskHandler(b.Logger)
	h.TaskHandler.TaskService = b.TaskService

	h.WriteHandler = NewWriteHandler(b.PublisherFn)
	h.WriteHandler.AuthorizationService = b.AuthorizationService
	h.WriteHandler.OrganizationService = b.OrganizationService
	h.WriteHandler.BucketService = b.BucketService
	h.WriteHandler.Logger = b.Logger.With(zap.String("handler", "write"))

	h.QueryHandler = NewFluxHandler()
	h.QueryHandler.AuthorizationService = b.AuthorizationService
	h.QueryHandler.OrganizationService = b.OrganizationService
	h.QueryHandler.Logger = b.Logger.With(zap.String("handler", "query"))
	h.QueryHandler.ProxyQueryService = pzap.NewProxyQueryService(h.QueryHandler.Logger)

	h.ChronografHandler = NewChronografHandler(b.ChronografService)

	return h
}

var apiLinks = map[string]interface{}{
	"signin":     "/api/v2/signin",
	"signout":    "/api/v2/signout",
	"setup":      "/api/v2/setup",
	"sources":    "/api/v2/sources",
	"dashboards": "/api/v2/dashboards",
	"query":      "/api/v2/query",
	"write":      "/api/v2/write",
	"orgs":       "/api/v2/orgs",
	"auths":      "/api/v2/authorizations",
	"buckets":    "/api/v2/buckets",
	"users":      "/api/v2/users",
	"tasks":      "/api/v2/tasks",
	"flux": map[string]string{
		"self":        "/api/v2/flux",
		"ast":         "/api/v2/flux/ast",
		"suggestions": "/api/v2/flux/suggestions",
	},
	"external": map[string]string{
		"statusFeed": "https://www.influxdata.com/feed/json",
	},
	"system": map[string]string{
		"metrics": "/metrics",
		"debug":   "/debug/pprof",
		"health":  "/healthz",
	},
}

func (h *APIHandler) serveLinks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := encodeResponse(ctx, w, http.StatusOK, apiLinks); err != nil {
		EncodeError(ctx, err, w)
		return
	}
}

// ServeHTTP delegates a request to the appropriate subhandler.
func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setCORSResponseHeaders(w, r)
	if r.Method == "OPTIONS" {
		return
	}

	// Serve the links base links for the API.
	if r.URL.Path == "/api/v2/" || r.URL.Path == "/api/v2" {
		h.serveLinks(w, r)
		return
	}

	if r.URL.Path == "/api/v2/signin" || r.URL.Path == "/api/v2/signout" {
		h.SessionHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/setup") {
		h.SetupHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/write") {
		h.WriteHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/query") {
		h.QueryHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/buckets") {
		h.BucketHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/users") {
		h.UserHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/orgs") {
		h.OrgHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/authorizations") {
		h.AuthorizationHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/dashboards") {
		h.DashboardHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/sources") {
		h.SourceHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/tasks") {
		h.TaskHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/views") {
		h.ViewHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/macros") {
		h.MacroHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/v2/flux") {
		h.FluxLangHandler.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.URL.Path, "/chronograf/") {
		h.ChronografHandler.ServeHTTP(w, r)
		return
	}

	http.NotFound(w, r)
}
