// Package main 提供 WebSocket 并发连接压测工具。
//
// 用法:
//
//	go run examples/bench/main.go -c 100 -m 10
//
// 参数:
//
//	-api     API 服务地址 (默认 http://localhost:8080)
//	-ws      WS 服务地址  (默认 ws://localhost:8081)
//	-c       并发连接数   (默认 100)
//	-m       每连接消息数 (默认 10)
//	-prefix  用户名前缀   (默认 benchuser)
//	-pw      统一密码     (默认 bench123)
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ─── 命令行参数 ─────────────────────────────────────────────

var (
	apiAddr    = flag.String("api", "http://localhost:8080", "API 服务地址")
	wsAddr     = flag.String("ws", "ws://localhost:8081", "WS 服务地址")
	concurrent = flag.Int("c", 100, "并发连接数")
	msgPerConn = flag.Int("m", 10, "每连接发送消息数")
	userPrefix = flag.String("prefix", "benchuser", "用户名前缀")
	password   = flag.String("pw", "bench123", "统一密码")
)

// ─── API 响应结构 ───────────────────────────────────────────

type apiResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type loginData struct {
	Token string `json:"token"`
}

// ─── WS 协议结构 ────────────────────────────────────────────

type clientMsg struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type chatData struct {
	MsgID       string `json:"msg_id"`
	ToID        int64  `json:"to_id"`
	ChatType    int    `json:"chat_type"`
	ContentType int    `json:"content_type"`
	Content     string `json:"content"`
}

type serverMsg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type ackData struct {
	MsgID string `json:"msg_id"`
}

// ─── 统计指标 ────────────────────────────────────────────────

type metrics struct {
	mu sync.Mutex

	// 连接阶段
	connectOK   int64
	connectFail int64
	connectDurs []time.Duration

	// 消息阶段
	msgSent int64
	ackRecv int64
	ackDurs []time.Duration // 从发送到收到 ACK 的延迟
}

func (m *metrics) addConnect(ok bool, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok {
		m.connectOK++
		m.connectDurs = append(m.connectDurs, d)
	} else {
		m.connectFail++
	}
}

func (m *metrics) addMsg() {
	atomic.AddInt64(&m.msgSent, 1)
}

func (m *metrics) addAck(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ackRecv++
	m.ackDurs = append(m.ackDurs, d)
}

// percentile 计算排序后切片的分位数（P50/P90/P99）
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (m *metrics) report(totalDur time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fmt.Println("\n╔═══════════════════════════════════════════════════╗")
	fmt.Println("║            WebSocket Benchmark Report             ║")
	fmt.Println("╠═══════════════════════════════════════════════════╣")
	fmt.Printf("║ Total Duration:    %-30s ║\n", totalDur.Round(time.Millisecond))
	fmt.Println("╠═══════════════════════════════════════════════════╣")
	fmt.Println("║ Connection Phase:                                 ║")
	fmt.Printf("║   Success:         %-30d ║\n", m.connectOK)
	fmt.Printf("║   Failed:          %-30d ║\n", m.connectFail)

	if len(m.connectDurs) > 0 {
		sort.Slice(m.connectDurs, func(i, j int) bool { return m.connectDurs[i] < m.connectDurs[j] })
		fmt.Printf("║   Avg Latency:     %-30s ║\n", avg(m.connectDurs).Round(time.Microsecond))
		fmt.Printf("║   P50 Latency:     %-30s ║\n", percentile(m.connectDurs, 50).Round(time.Microsecond))
		fmt.Printf("║   P90 Latency:     %-30s ║\n", percentile(m.connectDurs, 90).Round(time.Microsecond))
		fmt.Printf("║   P99 Latency:     %-30s ║\n", percentile(m.connectDurs, 99).Round(time.Microsecond))
	}

	fmt.Println("╠═══════════════════════════════════════════════════╣")
	fmt.Println("║ Message Phase:                                    ║")
	fmt.Printf("║   Messages Sent:   %-30d ║\n", m.msgSent)
	fmt.Printf("║   ACKs Received:   %-30d ║\n", m.ackRecv)

	if totalDur > 0 && m.msgSent > 0 {
		throughput := float64(m.msgSent) / totalDur.Seconds()
		fmt.Printf("║   Throughput:      %-26.1f msg/s ║\n", throughput)
	}

	if len(m.ackDurs) > 0 {
		sort.Slice(m.ackDurs, func(i, j int) bool { return m.ackDurs[i] < m.ackDurs[j] })
		fmt.Printf("║   ACK Avg Latency: %-30s ║\n", avg(m.ackDurs).Round(time.Microsecond))
		fmt.Printf("║   ACK P50:         %-30s ║\n", percentile(m.ackDurs, 50).Round(time.Microsecond))
		fmt.Printf("║   ACK P90:         %-30s ║\n", percentile(m.ackDurs, 90).Round(time.Microsecond))
		fmt.Printf("║   ACK P99:         %-30s ║\n", percentile(m.ackDurs, 99).Round(time.Microsecond))
	}

	fmt.Println("╚═══════════════════════════════════════════════════╝")
}

func avg(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return sum / time.Duration(len(ds))
}

// ─── HTTP 工具函数 ───────────────────────────────────────────

func registerUser(username, pw string) error {
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": pw,
	})
	resp, err := http.Post(*apiAddr+"/api/v1/user/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("register HTTP error: %w", err)
	}
	defer resp.Body.Close()

	var r apiResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}
	// code=0 成功, code=20001 用户已存在（压测重跑时可能发生），均视为成功
	if r.Code != 0 && r.Code != 20001 {
		return fmt.Errorf("register failed: code=%d msg=%s", r.Code, r.Msg)
	}
	return nil
}

func loginUser(username, pw string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": pw,
	})
	resp, err := http.Post(*apiAddr+"/api/v1/user/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("login HTTP error: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var r apiResp
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("decode login response: %w", err)
	}
	if r.Code != 0 {
		return "", fmt.Errorf("login failed: code=%d msg=%s", r.Code, r.Msg)
	}

	var ld loginData
	if err := json.Unmarshal(r.Data, &ld); err != nil {
		return "", fmt.Errorf("decode login data: %w", err)
	}
	return ld.Token, nil
}

// ─── 单个客户端 worker ──────────────────────────────────────

func runClient(id int, token string, m *metrics, wgDone *sync.WaitGroup) {
	defer wgDone.Done()

	// 1. 建立 WS 连接
	url := fmt.Sprintf("%s/ws?token=%s", *wsAddr, token)
	t0 := time.Now()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	connDur := time.Since(t0)

	if err != nil {
		m.addConnect(false, connDur)
		log.Printf("[client %d] connect failed: %v", id, err)
		return
	}
	m.addConnect(true, connDur)
	defer conn.Close()

	// 用 map 记录每条消息的发送时间，以便计算 ACK 延迟
	pending := make(map[string]time.Time)
	var mu sync.Mutex

	// 2. 启动读协程，收集 ACK
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var sm serverMsg
			if err := json.Unmarshal(raw, &sm); err != nil {
				continue
			}
			if sm.Type == "ack" {
				var ad ackData
				if err := json.Unmarshal(sm.Data, &ad); err != nil {
					continue
				}
				mu.Lock()
				if t, ok := pending[ad.MsgID]; ok {
					m.addAck(time.Since(t))
					delete(pending, ad.MsgID)
				}
				mu.Unlock()
			}
		}
	}()

	// 3. 发送消息（目标设为自己的 ID+1，不需要对方真实存在，只要 Kafka 能接收即可）
	targetID := int64(id + 1)
	for i := 0; i < *msgPerConn; i++ {
		msgID := fmt.Sprintf("bench-%d-%d", id, i)
		msg := clientMsg{
			Type: "chat",
			Data: chatData{
				MsgID:       msgID,
				ToID:        targetID,
				ChatType:    1,
				ContentType: 1,
				Content:     fmt.Sprintf("bench message %d from client %d", i, id),
			},
		}
		data, _ := json.Marshal(msg)

		mu.Lock()
		pending[msgID] = time.Now()
		mu.Unlock()

		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[client %d] write failed at msg %d: %v", id, i, err)
			break
		}
		m.addMsg()
	}

	// 4. 等待 ACK 回收（最多等 10 秒）
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()

	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-timer.C:
			mu.Lock()
			remaining := len(pending)
			mu.Unlock()
			if remaining > 0 {
				log.Printf("[client %d] timeout: %d ACKs not received", id, remaining)
			}
			return
		case <-tick.C:
			mu.Lock()
			remaining := len(pending)
			mu.Unlock()
			if remaining == 0 {
				return
			}
		case <-readDone:
			return
		}
	}
}

// ─── main ───────────────────────────────────────────────────

func main() {
	flag.Parse()

	n := *concurrent
	log.Printf("=== WebSocket Benchmark ===")
	log.Printf("API: %s | WS: %s", *apiAddr, *wsAddr)
	log.Printf("Concurrent: %d | Messages/conn: %d | Total messages: %d", n, *msgPerConn, n**msgPerConn)
	log.Println()

	// Phase 1: 注册 + 登录，获取 tokens
	log.Println("[phase 1] Registering and logging in users...")
	tokens := make([]string, n)
	var regWg sync.WaitGroup
	var regFail int64
	for i := 0; i < n; i++ {
		regWg.Add(1)
		go func(idx int) {
			defer regWg.Done()
			username := fmt.Sprintf("%s%d", *userPrefix, idx)
			if err := registerUser(username, *password); err != nil {
				log.Printf("[register] user %s failed: %v", username, err)
				atomic.AddInt64(&regFail, 1)
				return
			}
			token, err := loginUser(username, *password)
			if err != nil {
				log.Printf("[login] user %s failed: %v", username, err)
				atomic.AddInt64(&regFail, 1)
				return
			}
			tokens[idx] = token
		}(i)
	}
	regWg.Wait()

	if regFail > 0 {
		log.Printf("[phase 1] WARNING: %d users failed to register/login", regFail)
	}

	// 过滤出有效 token
	var validTokens []struct {
		id    int
		token string
	}
	for i, t := range tokens {
		if t != "" {
			validTokens = append(validTokens, struct {
				id    int
				token string
			}{i, t})
		}
	}
	log.Printf("[phase 1] Ready: %d/%d users authenticated\n", len(validTokens), n)

	if len(validTokens) == 0 {
		log.Fatal("No users authenticated, aborting benchmark")
	}

	// Phase 2: 并发建立 WS 连接 + 发送消息
	log.Println("[phase 2] Starting WebSocket connections and sending messages...")
	m := &metrics{}
	var wg sync.WaitGroup
	start := time.Now()

	for _, vt := range validTokens {
		wg.Add(1)
		go runClient(vt.id, vt.token, m, &wg)
	}

	wg.Wait()
	totalDur := time.Since(start)

	// Phase 3: 输出报告
	m.report(totalDur)
}
