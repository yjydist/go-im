package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/yjydist/go-im/internal/pkg/errcode"
)

type testResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSuccess_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Success(c, nil)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp testResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Code != errcode.Success {
		t.Errorf("expected code=%d, got %d", errcode.Success, resp.Code)
	}
	if resp.Msg != "success" {
		t.Errorf("expected msg=success, got %s", resp.Msg)
	}
}

func TestSuccess_WithData(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Success(c, map[string]string{"key": "value"})

	var resp testResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Code != errcode.Success {
		t.Errorf("expected code=%d, got %d", errcode.Success, resp.Code)
	}

	var data map[string]string
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatalf("failed to decode data: %v", err)
	}
	if data["key"] != "value" {
		t.Errorf("expected data.key=value, got %s", data["key"])
	}
}

func TestError_KnownCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Error(c, errcode.ErrBadRequest)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp testResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d, got %d", errcode.ErrBadRequest, resp.Code)
	}
	if resp.Msg != "bad request" {
		t.Errorf("expected msg=bad request, got %s", resp.Msg)
	}
}

func TestErrorWithMsg_CustomMessage(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ErrorWithMsg(c, errcode.ErrBadRequest, "username is required")

	var resp testResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Code != errcode.ErrBadRequest {
		t.Errorf("expected code=%d, got %d", errcode.ErrBadRequest, resp.Code)
	}
	if resp.Msg != "username is required" {
		t.Errorf("expected msg=username is required, got %s", resp.Msg)
	}
}
