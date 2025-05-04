package api

import (
	"github.com/gin-gonic/gin"
)

func SetupRoutes(router *gin.Engine) {
	router.GET("/status", GetStatus)

    router.POST("/cluster/register", ClusterRegister)
    router.POST("/cluster/deregister", ClusterDeregister)
    router.POST("/cluster/status/:cluster_id", SetClusterStatus)
    
    router.GET("/config", GetConfig)
    router.POST("/actions/config", SetConfig)
    router.POST("/actions/rollout", SetRollout)
    router.POST("/actions/reshard", SetReshard)
}