package Proxy

import (
	"log/slog"
	"net/http"
	"pluralkit/manager/internal/core"

	"github.com/go-chi/chi/v5"
)

// Helper struct for Proxy
type Proxy struct {
	httpClient      http.Client
	Config          core.ProxyConfig
	Logger          *slog.Logger
	EndpointsConfig core.EndpointsConfig
}

// Helper func for creating an Proxy struct
func NewProxy(config core.ProxyConfig, logger *slog.Logger) *Proxy {
	moduleLogger := logger.With(slog.String("module", "Proxy"))
	return &Proxy{
		httpClient:      http.Client{},
		Config:          config,
		Logger:          moduleLogger,
		EndpointsConfig: core.EndpointsConfig{},
	}
}

// Sets up routes
func (a *Proxy) SetupRoutes(router *chi.Mux) {
	router.Get("/ping", a.Ping)
	router.Get("/guilds/{id}", a.GetCache)
	router.Get("/guilds/{id}/*", a.GetCache)

	router.Route("/endpoints", func(r chi.Router) {
		r.Get("/{idx}", a.GetEndpoint)
		r.Post("/{idx}", a.SetEndpoint)
		r.Get("/", a.GetEndpoints)
		r.Patch("/", a.PatchEndpoints)
	})

}
