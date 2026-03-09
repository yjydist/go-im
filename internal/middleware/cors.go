package middleware

import (
	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件。
// 当 allowedOrigins 为空时，使用 "*"（开发模式）；
// 否则根据请求 Origin 动态匹配白名单。
func CORS(allowedOrigins ...string) gin.HandlerFunc {
	allowAll := len(allowedOrigins) == 0

	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		var allowOrigin string
		switch {
		case allowAll:
			allowOrigin = "*"
		case origin != "":
			if _, ok := originSet[origin]; ok {
				allowOrigin = origin
			} else if _, ok := originSet["*"]; ok {
				allowOrigin = "*"
			}
		}

		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Max-Age", "86400")
			if allowOrigin != "*" {
				c.Header("Vary", "Origin")
				// 非通配模式下允许浏览器发送凭据（Cookie、Authorization 等）
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}
