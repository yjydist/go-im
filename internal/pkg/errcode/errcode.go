package errcode

// 错误码定义
// code = 0 表示成功，非 0 对应具体错误
const (
	Success       = 0
	ErrBadRequest = 10001 // 参数错误
	ErrUnAuth     = 10002 // 未认证
	ErrForbidden  = 10003 // 无权限
	ErrNotFound   = 10004 // 资源不存在
	ErrInternal   = 10005 // 服务器内部错误

	// 用户模块 2xxxx
	ErrUserExist     = 20001 // 用户已存在
	ErrUserNotFound  = 20002 // 用户不存在
	ErrPasswordWrong = 20003 // 密码错误
	ErrUserBanned    = 20004 // 用户被封禁
	ErrTokenExpired  = 20005 // Token 过期
	ErrTokenInvalid  = 20006 // Token 无效

	// 好友模块 3xxxx
	ErrFriendExist    = 30001 // 已是好友
	ErrFriendNotFound = 30002 // 好友关系不存在
	ErrFriendSelf     = 30003 // 不能添加自己为好友
	ErrFriendPending  = 30004 // 好友申请待确认

	// 群组模块 4xxxx
	ErrGroupNotFound  = 40001 // 群组不存在
	ErrGroupMember    = 40002 // 已在群中
	ErrGroupNotMember = 40003 // 不在群中

	// 消息模块 5xxxx
	ErrMsgDuplicate = 50001 // 消息重复
	ErrMsgSendFail  = 50002 // 消息发送失败
)

// errMsgMap 错误码对应消息
var errMsgMap = map[int]string{
	Success:           "success",
	ErrBadRequest:     "bad request",
	ErrUnAuth:         "unauthorized",
	ErrForbidden:      "forbidden",
	ErrNotFound:       "not found",
	ErrInternal:       "internal server error",
	ErrUserExist:      "user already exists",
	ErrUserNotFound:   "user not found",
	ErrPasswordWrong:  "wrong password",
	ErrUserBanned:     "user is banned",
	ErrTokenExpired:   "token expired",
	ErrTokenInvalid:   "invalid token",
	ErrFriendExist:    "already friends",
	ErrFriendNotFound: "friendship not found",
	ErrFriendSelf:     "cannot add yourself as friend",
	ErrFriendPending:  "friend request pending",
	ErrGroupNotFound:  "group not found",
	ErrGroupMember:    "already in group",
	ErrGroupNotMember: "not in group",
	ErrMsgDuplicate:   "duplicate message",
	ErrMsgSendFail:    "message send failed",
}

// GetMsg 获取错误码对应的消息
func GetMsg(code int) string {
	if msg, ok := errMsgMap[code]; ok {
		return msg
	}
	return "unknown error"
}
