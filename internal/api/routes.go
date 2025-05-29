package api

import (
	"log/slog"
	"net/http"
	"pluralkit/manager/internal/core"
	"pluralkit/manager/internal/etcd"

	"github.com/go-chi/chi/v5"
)

// Helper struct for API
type API struct {
	EtcdClient     *etcd.Client
	Controller     *core.Machine
	CacheEndpoints *[]string
	NumShards      *int
	httpClient     http.Client
	Config         core.ManagerConfig
	Logger         *slog.Logger
}

// Helper func for creating an API struct
func NewAPI(etcdCli *etcd.Client, controller *core.Machine, config core.ManagerConfig, logger *slog.Logger) *API {
	moduleLogger := logger.With(slog.String("module", "API"))
	return &API{
		EtcdClient:     etcdCli,
		Controller:     controller,
		CacheEndpoints: controller.GetCacheEndpoints(),
		NumShards:      controller.GetNumShards(),
		httpClient:     http.Client{},
		Config:         config,
		Logger:         moduleLogger,
	}
}

// Sets up routes
func (a *API) SetupRoutes(router *chi.Mux) {
	router.Get("/ping", a.Ping)
	router.Get("/status", a.GetStatus)

	router.Patch("/shard/status", a.SetShardStatus)

	router.Get("/config", a.GetConfig)
	router.Get("/config/next", a.GetNextConfig)

	router.Route("/actions", func(r chi.Router) {
		r.Post("/config", a.SetConfig)
		r.Post("/rollout", a.SetRollout)
		r.Post("/deploy", a.SetDeploy)
	})

	router.Get("/cache/guilds/{id}", a.GetCache)
	router.Get("/cache/guilds/{id}/*", a.GetCache)
}
