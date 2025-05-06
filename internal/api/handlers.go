package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func Ping(c *gin.Context) {
	c.String(http.StatusOK, "pong!")
}

func GetStatus(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func ClusterRegister(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func ClusterDeregister(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func SetClusterStatus(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func GetConfig(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func SetConfig(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func SetRollout(c *gin.Context) {
	c.String(http.StatusOK, "")
}

func SetReshard(c *gin.Context) {
	c.String(http.StatusOK, "")
}
