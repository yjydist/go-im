package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ==================== CORS 中间件测试 ====================

func TestCORS_SetsHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// 验证 CORS 头
	tests := []struct {
		header string
		want   string
	}{
		{"Access-Control-Allow-Origin", "*"},
		{"Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS"},
		{"Access-Control-Allow-Headers", "Content-Type, Authorization"},
		{"Access-Control-Max-Age", "86400"},
	}

	for _, tt := range tests {
		got := w.Header().Get(tt.header)
		if got != tt.want {
			t.Errorf("header %s: expected %q, got %q", tt.header, tt.want, got)
		}
	}
}

func TestCORS_OptionsPreflightReturns204(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS preflight, got %d", w.Code)
	}

	// Body 应为空
	if w.Body.Len() != 0 {
		t.Errorf("expected empty body for OPTIONS, got %s", w.Body.String())
	}
}

// ==================== Logger 中间件测试 ====================

func TestLogger_DoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	l, _ := zap.NewDevelopment()

	r := gin.New()
	r.Use(Logger(l))
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	req := httptest.NewRequest(http.MethodGet, "/ping?foo=bar", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "pong" {
		t.Errorf("expected body=pong, got %s", w.Body.String())
	}
}

func TestLogger_LogsPostRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	l, _ := zap.NewDevelopment()

	r := gin.New()
	r.Use(Logger(l))
	r.POST("/submit", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"status": "created"})
	})

	req := httptest.NewRequest(http.MethodPost, "/submit", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

func TestLogger_Logs404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	l, _ := zap.NewDevelopment()

	r := gin.New()
	r.Use(Logger(l))
	// 不注册任何路由

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
