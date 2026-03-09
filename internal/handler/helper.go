package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yjydist/go-im/internal/pkg/errcode"
	"github.com/yjydist/go-im/internal/pkg/response"
	"github.com/yjydist/go-im/internal/service"
	"go.uber.org/zap"
)

// parseID 解析字符串 ID 为 int64，拒绝非正整数
func parseID(s string, id *int64) (int64, error) {
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %s", s)
	}
	if val <= 0 {
		return 0, fmt.Errorf("invalid id: %s, must be positive", s)
	}
	if id != nil {
		*id = val
	}
	return val, nil
}

// handleServiceError 统一处理 service 层返回的错误。
// 优先识别业务错误码并返回对应响应，非业务错误返回 ErrInternal。
func handleServiceError(c *gin.Context, logger *zap.Logger, msg string, err error) {
	if code, isBiz := service.ParseBusinessError(err); isBiz {
		response.Error(c, code)
		return
	}
	logger.Error(msg, zap.Error(err))
	response.Error(c, errcode.ErrInternal)
}
