package errcode

import "testing"

func TestGetMsg_KnownCodes(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{Success, "success"},
		{ErrBadRequest, "bad request"},
		{ErrUnAuth, "unauthorized"},
		{ErrForbidden, "forbidden"},
		{ErrNotFound, "not found"},
		{ErrInternal, "internal server error"},
		{ErrUserExist, "user already exists"},
		{ErrUserNotFound, "user not found"},
		{ErrPasswordWrong, "wrong password"},
		{ErrUserBanned, "user is banned"},
		{ErrTokenExpired, "token expired"},
		{ErrTokenInvalid, "invalid token"},
		{ErrFriendExist, "already friends"},
		{ErrFriendNotFound, "friendship not found"},
		{ErrFriendSelf, "cannot add yourself as friend"},
		{ErrFriendPending, "friend request pending"},
		{ErrGroupNotFound, "group not found"},
		{ErrGroupMember, "already in group"},
		{ErrGroupNotMember, "not in group"},
		{ErrMsgDuplicate, "duplicate message"},
		{ErrMsgSendFail, "message send failed"},
	}

	for _, tt := range tests {
		got := GetMsg(tt.code)
		if got != tt.want {
			t.Errorf("GetMsg(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestGetMsg_UnknownCode(t *testing.T) {
	got := GetMsg(99999)
	if got != "unknown error" {
		t.Errorf("GetMsg(99999) = %q, want %q", got, "unknown error")
	}
}
