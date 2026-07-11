package api

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

func registerWebsocketRoutes(r *gin.Engine) {
	r.Group("")
	fmt.Println("registerWebsocketRoutes...")
}
