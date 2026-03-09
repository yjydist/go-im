package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/yjydist/go-im/internal/config"
	"github.com/yjydist/go-im/internal/model"
	"github.com/yjydist/go-im/internal/pkg/errcode"
	pkgjwt "github.com/yjydist/go-im/internal/pkg/jwt"
	"github.com/yjydist/go-im/internal/pkg/snowflake"
	"github.com/yjydist/go-im/internal/repository"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testJWTSecret = "test-handler-secret"

// apiResp 统一响应结构（反序列化用）
type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// setupHandlerTestEnv 初始化 handler 测试环境：SQLite + miniredis + snowflake + JWT config
func setupHandlerTestEnv(t *testing.T) {
	t.Helper()

	// SQLite in-memory
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	err = db.AutoMigrate(
		&model.User{},
		&model.Friendship{},
		&model.Group{},
		&model.GroupMember{},
		&model.Message{},
		&model.OfflineMessage{},
	)
	if err != nil {
		t.Fatalf("failed to auto migrate: %v", err)
	}
	repository.DB = db

	// miniredis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(func() { mr.Close() })
	repository.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// Snowflake
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("failed to init snowflake: %v", err)
	}

	// JWT config
	config.GlobalConfig = &config.Config{
		JWT: config.JWTConfig{
			Secret:            testJWTSecret,
			AccessExpireHours: 24,
		},
	}
}

// setupRouter 创建带完整路由注册的测试路由
func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	l, _ := zap.NewDevelopment()
	RegisterRoutes(r, l)
	return r
}

// generateTestToken 生成测试用 JWT token
func generateTestToken(t *testing.T, userID int64) string {
	t.Helper()
	token, err := pkgjwt.GenerateToken(userID, testJWTSecret, 24)
	if err != nil {
		t.Fatalf("failed to generate test token: %v", err)
	}
	return token
}

// jsonBody 将对象序列化为 *bytes.Reader
func jsonBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal json: %v", err)
	}
	return bytes.NewReader(data)
}

// doRequest 发起 HTTP 请求并返回 apiResp
func doRequest(t *testing.T, r *gin.Engine, method, path string, body *bytes.Reader, token string) (*httptest.ResponseRecorder, apiResp) {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp apiResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v\nbody: %s", err, w.Body.String())
	}
	return w, resp
}

// registerUser 注册用户并返回 user_id 和 token
func registerAndLogin(t *testing.T, r *gin.Engine, username, password, nickname string) (int64, string) {
	t.Helper()

	// 注册
	regBody := jsonBody(t, RegisterRequest{
		Username: username,
		Password: password,
		Nickname: nickname,
	})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/register", regBody, "")
	if resp.Code != errcode.Success {
		t.Fatalf("register %s failed: code=%d msg=%s", username, resp.Code, resp.Msg)
	}

	// 登录
	loginBody := jsonBody(t, LoginRequest{
		Username: username,
		Password: password,
	})
	_, loginResp := doRequest(t, r, http.MethodPost, "/api/v1/user/login", loginBody, "")
	if loginResp.Code != errcode.Success {
		t.Fatalf("login %s failed: code=%d msg=%s", username, loginResp.Code, loginResp.Msg)
	}

	var loginData LoginResponse
	if err := json.Unmarshal(loginResp.Data, &loginData); err != nil {
		t.Fatalf("failed to decode login data: %v", err)
	}

	// 从 token 中解析 user_id
	claims, err := pkgjwt.ParseToken(loginData.Token, testJWTSecret)
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	return claims.UserID, loginData.Token
}

// ==================== User Handler 测试 ====================

func TestHandler_Register_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	body := jsonBody(t, RegisterRequest{
		Username: "newuser",
		Password: "password123",
		Nickname: "New User",
	})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/register", body, "")

	if resp.Code != errcode.Success {
		t.Errorf("expected code=%d, got code=%d msg=%s", errcode.Success, resp.Code, resp.Msg)
	}
}

func TestHandler_Register_BadRequest(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	// 缺少 required 字段 username
	body := jsonBody(t, map[string]string{"password": "123456"})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/register", body, "")

	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_Register_ShortPassword(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	// 密码太短（min=6）
	body := jsonBody(t, RegisterRequest{
		Username: "shortpw",
		Password: "12",
	})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/register", body, "")

	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_Register_Duplicate(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	body := jsonBody(t, RegisterRequest{Username: "dupuser", Password: "password123"})
	doRequest(t, r, http.MethodPost, "/api/v1/user/register", body, "")

	// 重复注册
	body2 := jsonBody(t, RegisterRequest{Username: "dupuser", Password: "password456"})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/register", body2, "")

	if resp.Code != errcode.ErrUserExist {
		t.Errorf("expected code=%d (ErrUserExist), got code=%d", errcode.ErrUserExist, resp.Code)
	}
}

func TestHandler_Login_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	// 先注册
	regBody := jsonBody(t, RegisterRequest{Username: "loginuser", Password: "mypassword"})
	doRequest(t, r, http.MethodPost, "/api/v1/user/register", regBody, "")

	// 登录
	loginBody := jsonBody(t, LoginRequest{Username: "loginuser", Password: "mypassword"})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/login", loginBody, "")

	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var data LoginResponse
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	if data.Token == "" {
		t.Error("expected non-empty token")
	}
}

func TestHandler_Login_WrongPassword(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	regBody := jsonBody(t, RegisterRequest{Username: "wrongpw", Password: "correctpw"})
	doRequest(t, r, http.MethodPost, "/api/v1/user/register", regBody, "")

	loginBody := jsonBody(t, LoginRequest{Username: "wrongpw", Password: "wrong"})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/login", loginBody, "")

	if resp.Code != errcode.ErrPasswordWrong {
		t.Errorf("expected code=%d (ErrPasswordWrong), got code=%d", errcode.ErrPasswordWrong, resp.Code)
	}
}

func TestHandler_Login_UserNotFound(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	loginBody := jsonBody(t, LoginRequest{Username: "ghost", Password: "whatever"})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/login", loginBody, "")

	if resp.Code != errcode.ErrUserNotFound {
		t.Errorf("expected code=%d (ErrUserNotFound), got code=%d", errcode.ErrUserNotFound, resp.Code)
	}
}

func TestHandler_Login_BadRequest(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	// 空 body
	body := jsonBody(t, map[string]string{})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/user/login", body, "")

	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetUserInfo_Self(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	userID, token := registerAndLogin(t, r, "infoself", "password123", "InfoSelf")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/user/info", nil, token)
	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var user model.User
	if err := json.Unmarshal(resp.Data, &user); err != nil {
		t.Fatalf("failed to decode user: %v", err)
	}
	if user.ID != userID {
		t.Errorf("expected user_id=%d, got %d", userID, user.ID)
	}
	if user.Username != "infoself" {
		t.Errorf("expected username=infoself, got %s", user.Username)
	}
}

func TestHandler_GetUserInfo_Other(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "viewer", "password123", "Viewer")
	targetID, _ := registerAndLogin(t, r, "target", "password123", "Target")

	path := "/api/v1/user/info?user_id=" + strconv.FormatInt(targetID, 10)
	_, resp := doRequest(t, r, http.MethodGet, path, nil, token)
	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var user model.User
	if err := json.Unmarshal(resp.Data, &user); err != nil {
		t.Fatalf("failed to decode user: %v", err)
	}
	if user.ID != targetID {
		t.Errorf("expected user_id=%d, got %d", targetID, user.ID)
	}
}

func TestHandler_GetUserInfo_InvalidUserID(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "badid", "password123", "BadID")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/user/info?user_id=abc", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetUserInfo_NoAuth(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/user/info", nil, "")
	if resp.Code != errcode.ErrUnAuth {
		t.Errorf("expected code=%d (ErrUnAuth), got code=%d", errcode.ErrUnAuth, resp.Code)
	}
}

// ==================== Friend Handler 测试 ====================

func TestHandler_AddFriend_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, tokenA := registerAndLogin(t, r, "alice", "password123", "Alice")
	idB, _ := registerAndLogin(t, r, "bob", "password123", "Bob")

	body := jsonBody(t, AddFriendRequest{FriendID: idB})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/friend/add", body, tokenA)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestHandler_AddFriend_BadRequest(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "friendbad", "password123", "FB")

	// 缺少 friend_id
	body := jsonBody(t, map[string]string{})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/friend/add", body, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_AddFriend_Self(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	idA, tokenA := registerAndLogin(t, r, "selfadd", "password123", "SelfAdd")

	body := jsonBody(t, AddFriendRequest{FriendID: idA})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/friend/add", body, tokenA)
	if resp.Code != errcode.ErrFriendSelf {
		t.Errorf("expected code=%d (ErrFriendSelf), got code=%d", errcode.ErrFriendSelf, resp.Code)
	}
}

func TestHandler_AcceptFriend_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	idA, tokenA := registerAndLogin(t, r, "requser", "password123", "Req")
	idB, tokenB := registerAndLogin(t, r, "accuser", "password123", "Acc")

	// A -> B 发送好友申请
	addBody := jsonBody(t, AddFriendRequest{FriendID: idB})
	doRequest(t, r, http.MethodPost, "/api/v1/friend/add", addBody, tokenA)

	// B 接受 A 的申请
	acceptBody := jsonBody(t, AcceptFriendRequest{FriendID: idA})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/friend/accept", acceptBody, tokenB)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestHandler_ListFriends_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	idA, tokenA := registerAndLogin(t, r, "listA", "password123", "ListA")
	idB, tokenB := registerAndLogin(t, r, "listB", "password123", "ListB")

	// A -> B 好友请求
	addBody := jsonBody(t, AddFriendRequest{FriendID: idB})
	doRequest(t, r, http.MethodPost, "/api/v1/friend/add", addBody, tokenA)

	// B 接受
	acceptBody := jsonBody(t, AcceptFriendRequest{FriendID: idA})
	doRequest(t, r, http.MethodPost, "/api/v1/friend/accept", acceptBody, tokenB)

	// A 查好友列表
	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/friend/list", nil, tokenA)
	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var friends []model.User
	if err := json.Unmarshal(resp.Data, &friends); err != nil {
		t.Fatalf("failed to decode friends: %v", err)
	}
	found := false
	for _, f := range friends {
		if f.ID == idB {
			found = true
		}
	}
	if !found {
		t.Errorf("expected listB in friend list, got %+v", friends)
	}
}

func TestHandler_ListFriends_Empty(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "nofriend", "password123", "NoFriend")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/friend/list", nil, token)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

// ==================== Group Handler 测试 ====================

func TestHandler_CreateGroup_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "groupowner", "password123", "Owner")

	body := jsonBody(t, CreateGroupRequest{Name: "TestGroup"})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/group/create", body, token)
	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var group model.Group
	if err := json.Unmarshal(resp.Data, &group); err != nil {
		t.Fatalf("failed to decode group: %v", err)
	}
	if group.Name != "TestGroup" {
		t.Errorf("expected name=TestGroup, got %s", group.Name)
	}
}

func TestHandler_CreateGroup_BadRequest(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "grpbad", "password123", "GrpBad")

	// 缺少 name
	body := jsonBody(t, map[string]string{})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/group/create", body, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_JoinGroup_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, tokenOwner := registerAndLogin(t, r, "jgowner", "password123", "JGOwner")
	_, tokenMember := registerAndLogin(t, r, "jgmember", "password123", "JGMember")

	// 创建群
	createBody := jsonBody(t, CreateGroupRequest{Name: "JoinGroup"})
	_, createResp := doRequest(t, r, http.MethodPost, "/api/v1/group/create", createBody, tokenOwner)
	var group model.Group
	json.Unmarshal(createResp.Data, &group)

	// 成员加入
	joinBody := jsonBody(t, JoinGroupRequest{GroupID: group.ID})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/group/join", joinBody, tokenMember)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestHandler_JoinGroup_NotFound(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "jgnotfound", "password123", "JGNotFound")

	body := jsonBody(t, JoinGroupRequest{GroupID: 99999})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/group/join", body, token)
	if resp.Code != errcode.ErrGroupNotFound {
		t.Errorf("expected code=%d (ErrGroupNotFound), got code=%d", errcode.ErrGroupNotFound, resp.Code)
	}
}

func TestHandler_JoinGroup_AlreadyMember(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, tokenOwner := registerAndLogin(t, r, "dupowner", "password123", "DupOwner")
	_, tokenMember := registerAndLogin(t, r, "dupmem", "password123", "DupMem")

	createBody := jsonBody(t, CreateGroupRequest{Name: "DupJoinGroup"})
	_, createResp := doRequest(t, r, http.MethodPost, "/api/v1/group/create", createBody, tokenOwner)
	var group model.Group
	json.Unmarshal(createResp.Data, &group)

	// 第一次加入
	joinBody := jsonBody(t, JoinGroupRequest{GroupID: group.ID})
	doRequest(t, r, http.MethodPost, "/api/v1/group/join", joinBody, tokenMember)

	// 重复加入
	joinBody2 := jsonBody(t, JoinGroupRequest{GroupID: group.ID})
	_, resp := doRequest(t, r, http.MethodPost, "/api/v1/group/join", joinBody2, tokenMember)
	if resp.Code != errcode.ErrGroupMember {
		t.Errorf("expected code=%d (ErrGroupMember), got code=%d", errcode.ErrGroupMember, resp.Code)
	}
}

func TestHandler_ListMyGroups_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "lister", "password123", "Lister")

	// 创建两个群
	createBody1 := jsonBody(t, CreateGroupRequest{Name: "Group1"})
	doRequest(t, r, http.MethodPost, "/api/v1/group/create", createBody1, token)
	createBody2 := jsonBody(t, CreateGroupRequest{Name: "Group2"})
	doRequest(t, r, http.MethodPost, "/api/v1/group/create", createBody2, token)

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/group/list", nil, token)
	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var groups []model.Group
	if err := json.Unmarshal(resp.Data, &groups); err != nil {
		t.Fatalf("failed to decode groups: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
}

func TestHandler_ListMembers_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, tokenOwner := registerAndLogin(t, r, "memowner", "password123", "MemOwner")
	_, tokenMember := registerAndLogin(t, r, "memjoin", "password123", "MemJoin")

	createBody := jsonBody(t, CreateGroupRequest{Name: "MembersGroup"})
	_, createResp := doRequest(t, r, http.MethodPost, "/api/v1/group/create", createBody, tokenOwner)
	var group model.Group
	json.Unmarshal(createResp.Data, &group)

	joinBody := jsonBody(t, JoinGroupRequest{GroupID: group.ID})
	doRequest(t, r, http.MethodPost, "/api/v1/group/join", joinBody, tokenMember)

	path := "/api/v1/group/members?group_id=" + strconv.FormatInt(group.ID, 10)
	_, resp := doRequest(t, r, http.MethodGet, path, nil, tokenOwner)
	if resp.Code != errcode.Success {
		t.Fatalf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}

	var members []model.GroupMember
	if err := json.Unmarshal(resp.Data, &members); err != nil {
		t.Fatalf("failed to decode members: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members (owner + joiner), got %d", len(members))
	}
}

func TestHandler_ListMembers_MissingGroupID(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "noid", "password123", "NoID")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/group/members", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_ListMembers_InvalidGroupID(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "badgid", "password123", "BadGID")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/group/members?group_id=abc", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

// ==================== Message Handler 测试 ====================

func TestHandler_GetOfflineMessages_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "offuser", "password123", "OffUser")

	// 没有离线消息也应该成功
	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/offline", nil, token)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestHandler_GetHistory_Success(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "histuser", "password123", "HistUser")

	// 没有历史消息也应该成功
	path := "/api/v1/message/history?target_id=2&chat_type=1"
	_, resp := doRequest(t, r, http.MethodGet, path, nil, token)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestHandler_GetHistory_MissingTargetID(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "notarget", "password123", "NoTarget")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/history?chat_type=1", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetHistory_MissingChatType(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "nochat", "password123", "NoChat")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/history?target_id=2", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetHistory_InvalidChatType(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "badchat", "password123", "BadChat")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/history?target_id=2&chat_type=3", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetHistory_InvalidTargetID(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "badtid", "password123", "BadTID")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/history?target_id=abc&chat_type=1", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetHistory_WithParams(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "paramuser", "password123", "ParamUser")

	path := "/api/v1/message/history?target_id=2&chat_type=1&cursor_msg_id=100&limit=10"
	_, resp := doRequest(t, r, http.MethodGet, path, nil, token)
	if resp.Code != errcode.Success {
		t.Errorf("expected success, got code=%d msg=%s", resp.Code, resp.Msg)
	}
}

func TestHandler_GetHistory_InvalidCursorMsgID(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "badcursor", "password123", "BadCursor")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/history?target_id=2&chat_type=1&cursor_msg_id=xyz", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

func TestHandler_GetHistory_InvalidLimit(t *testing.T) {
	setupHandlerTestEnv(t)
	r := setupRouter()

	_, token := registerAndLogin(t, r, "badlimit", "password123", "BadLimit")

	_, resp := doRequest(t, r, http.MethodGet, "/api/v1/message/history?target_id=2&chat_type=1&limit=abc", nil, token)
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d (ErrBadRequest), got code=%d", errcode.ErrBadRequest, resp.Code)
	}
}

// ==================== parseID 测试 ====================

func TestParseID_Valid(t *testing.T) {
	var id int64
	val, err := parseID("12345", &id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 12345 || id != 12345 {
		t.Errorf("expected 12345, got val=%d id=%d", val, id)
	}
}

func TestParseID_Invalid(t *testing.T) {
	_, err := parseID("notanumber", nil)
	if err == nil {
		t.Fatal("expected error for invalid id, got nil")
	}
}

func TestParseID_NilPointer(t *testing.T) {
	val, err := parseID("99", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 99 {
		t.Errorf("expected 99, got %d", val)
	}
}
