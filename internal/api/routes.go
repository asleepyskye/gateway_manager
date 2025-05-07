package api

import (
	"pluralkit/manager/internal/core"
	"pluralkit/manager/internal/etcd"

	"github.com/gin-gonic/gin"
)

type API struct {
	EtcdClient *etcd.Client
	Controller *core.Machine
}

func NewAPI(etcdCli *etcd.Client, controller *core.Machine) *API {
	return &API{
		EtcdClient: etcdCli,
		Controller: controller,
	}
}

// TODO: document this function.
func (a *API) SetupRoutes(router *gin.Engine) {
	router.GET("/ping", a.Ping)
	router.GET("/status", a.GetStatus)

	router.POST("/cluster/register", a.ClusterRegister)
	router.POST("/cluster/deregister", a.ClusterDeregister)
	router.POST("/cluster/status/:cluster_id", a.SetClusterStatus)

	router.GET("/config", a.GetConfig)
	router.POST("/actions/config", a.SetConfig)
	router.POST("/actions/rollout", a.SetRollout)
	router.POST("/actions/deploy", a.SetDeploy)
}
