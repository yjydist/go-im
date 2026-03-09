package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/yjydist/go-im/internal/middleware"
	"github.com/yjydist/go-im/internal/pkg/errcode"
	"github.com/yjydist/go-im/internal/pkg/response"
	"github.com/yjydist/go-im/internal/service"
	"go.uber.org/zap"
)

// MessageHandler 消息 Handler
type MessageHandler struct {
	msgService service.MessageService
	logger     *zap.Logger
}

// NewMessageHandler 创建消息 Handler
func NewMessageHandler(logger *zap.Logger) *MessageHandler {
	return &MessageHandler{
		msgService: service.NewMessageService(logger),
		logger:     logger,
	}
}

// GetOfflineMessages 拉取离线消息
// @Summary 拉取离线消息
// @Tags 消息
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/message/offline [get]
func (h *MessageHandler) GetOfflineMessages(c *gin.Context) {
	userID := middleware.GetUserID(c)
	messages, err := h.msgService.GetOfflineMessages(c.Request.Context(), userID)
	if err != nil {
		h.logger.Error("get offline messages failed", zap.Error(err))
		response.Error(c, errcode.ErrInternal)
		return
	}

	response.Success(c, messages)
}

// GetHistory 拉取历史消息
// @Summary 游标分页拉取历史消息
// @Tags 消息
// @Produce json
// @Param target_id query int64 true "目标ID（用户ID或群组ID）"
// @Param chat_type query int true "聊天类型 1:单聊 2:群聊"
// @Param cursor_msg_id query int64 false "游标消息ID"
// @Param limit query int false "每页数量（默认20，最大100）"
// @Success 200 {object} response.Response
// @Router /api/v1/message/history [get]
func (h *MessageHandler) GetHistory(c *gin.Context) {
	userID := middleware.GetUserID(c)

	targetIDStr := c.Query("target_id")
	if targetIDStr == "" {
		response.ErrorWithMsg(c, errcode.ErrBadRequest, "target_id is required")
		return
	}
	targetID, err := strconv.ParseInt(targetIDStr, 10, 64)
	if err != nil {
		response.ErrorWithMsg(c, errcode.ErrBadRequest, "invalid target_id")
		return
	}

	chatTypeStr := c.Query("chat_type")
	if chatTypeStr == "" {
		response.ErrorWithMsg(c, errcode.ErrBadRequest, "chat_type is required")
		return
	}
	chatType, err := strconv.Atoi(chatTypeStr)
	if err != nil || (chatType != 1 && chatType != 2) {
		response.ErrorWithMsg(c, errcode.ErrBadRequest, "invalid chat_type, must be 1 or 2")
		return
	}

	var cursorMsgID int64
	if cursorStr := c.Query("cursor_msg_id"); cursorStr != "" {
		cursorMsgID, err = strconv.ParseInt(cursorStr, 10, 64)
		if err != nil {
			response.ErrorWithMsg(c, errcode.ErrBadRequest, "invalid cursor_msg_id")
			return
		}
	}

	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			response.ErrorWithMsg(c, errcode.ErrBadRequest, "invalid limit")
			return
		}
	}

	messages, err := h.msgService.GetHistory(c.Request.Context(), userID, targetID, int8(chatType), cursorMsgID, limit)
	if err != nil {
		h.logger.Error("get history failed", zap.Error(err))
		response.Error(c, errcode.ErrInternal)
		return
	}

	response.Success(c, messages)
}
