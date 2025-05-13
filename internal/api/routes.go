package api

import (
	"net/http"
	"pluralkit/manager/internal/core"
	"pluralkit/manager/internal/etcd"

	"github.com/gin-gonic/gin"
)

// TODO: document this struct.
type API struct {
	EtcdClient     *etcd.Client
	Controller     *core.Machine
	CacheEndpoints *[]string
	NumShards      *int
	httpClient     http.Client
	Config         core.ManagerConfig
}

// TODO: document this function.
func NewAPI(etcdCli *etcd.Client, controller *core.Machine, config core.ManagerConfig) *API {
	return &API{
		EtcdClient:     etcdCli,
		Controller:     controller,
		CacheEndpoints: controller.GetCacheEndpoints(),
		NumShards:      controller.GetNumShards(),
		httpClient:     http.Client{},
		Config:         config,
	}
}

// TODO: document this function.
func (a *API) SetupRoutes(router *gin.Engine) {
	router.GET("/ping", a.Ping)
	router.GET("/status", a.GetStatus)

	router.POST("/shard/status", a.SetShardStatus)

	router.GET("/config", a.GetConfig)
	router.POST("/actions/config", a.SetConfig)
	router.POST("/actions/rollout", a.SetRollout)
	router.POST("/actions/deploy", a.SetDeploy)

	router.GET("/cache/guilds/:id/*path", a.GetCache)
}
