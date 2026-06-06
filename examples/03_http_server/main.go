// http_server 演示如何将图引擎暴露为工业级 HTTP REST 服务。
//
// 特性:
//   - 结构化日志 (log/slog)
//   - 请求指标 (/metrics)
//   - X-Request-ID 追踪
//   - Panic 恢复
//   - 并发限流
//   - 优雅关闭
//
//  用法:
//    go run ./examples/03_http_server
//
//  测试:
//    # 正常订单
//    curl -X POST http://localhost:8080/order-pipeline \
//      -H 'Content-Type: application/json' \
//      -d '{"order_id":"ORD-001","amount":299.00}'
//
//    # 大额订单（支付失败）
//    curl -X POST http://localhost:8080/order-pipeline \
//      -H 'Content-Type: application/json' \
//      -d '{"order_id":"ORD-002","amount":99999.00}'
//
//    # 传入 X-Request-ID
//    curl -X POST http://localhost:8080/order-pipeline \
//      -H 'Content-Type: application/json' \
//      -H 'X-Request-ID: my-trace-001' \
//      -d '{"order_id":"ORD-003","amount":59.90}'
//
//    # 健康检查
//    curl http://localhost:8080/health
//
//    # 指标
//    curl http://localhost:8080/metrics | jq
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/wzhongyou/graphflow/graph"
	"github.com/wzhongyou/graphflow/server"
)

// OrderState 订单状态。
type OrderState struct {
	OrderID   string   `json:"order_id"`
	Amount    float64  `json:"amount"`
	Validated bool     `json:"validated"`
	Paid      bool     `json:"paid"`
	Fulfilled bool     `json:"fulfilled"`
	Notified  bool     `json:"notified"`
	Error     string   `json:"error"`
	Steps     []string `json:"steps"`
}

func validateOrder(ctx context.Context, s OrderState) (OrderState, error) {
	slog.DebugContext(ctx, "validating order", "order_id", s.OrderID)
	time.Sleep(100 * time.Millisecond)
	if s.Amount <= 0 {
		s.Error = "订单金额无效"
		return s, fmt.Errorf("订单金额无效: %.2f", s.Amount)
	}
	s.Validated = true
	s.Steps = append(s.Steps, "validated")
	return s, nil
}

func processPayment(ctx context.Context, s OrderState) (OrderState, error) {
	slog.DebugContext(ctx, "processing payment", "amount", s.Amount)
	time.Sleep(200 * time.Millisecond)
	if s.Amount > 10000 {
		s.Error = "余额不足"
		s.Steps = append(s.Steps, "payment_failed")
		return s, fmt.Errorf("支付失败: 余额不足")
	}
	s.Paid = true
	s.Steps = append(s.Steps, "paid")
	return s, nil
}

func fulfillOrder(ctx context.Context, s OrderState) (OrderState, error) {
	time.Sleep(150 * time.Millisecond)
	s.Fulfilled = true
	s.Steps = append(s.Steps, "fulfilled")
	return s, nil
}

func cancelOrder(ctx context.Context, s OrderState) (OrderState, error) {
	time.Sleep(50 * time.Millisecond)
	s.Steps = append(s.Steps, "cancelled")
	return s, nil
}

func sendNotification(ctx context.Context, s OrderState) (OrderState, error) {
	time.Sleep(80 * time.Millisecond)
	s.Notified = true
	s.Steps = append(s.Steps, "notified")
	return s, nil
}

func isPaymentFailed(ctx context.Context, s OrderState) bool {
	return s.Error != "" && !s.Paid
}

func main() {
	// 1. 设置结构化日志（JSON 格式，便于日志采集）。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 2. 构建图。
	g := graph.NewGraph[OrderState]("order-pipeline")
	g.AddNode("validate", validateOrder)
	g.AddNode("payment", processPayment)
	g.AddNode("fulfill", fulfillOrder)
	g.AddNode("cancel", cancelOrder)
	g.AddNode("notify", sendNotification)
	g.SetEntryPoint("validate")
	g.AddEdge("validate", "payment")
	g.AddCondition("payment", graph.Condition[OrderState]{
		If:     isPaymentFailed,
		Target: "cancel",
	})
	g.AddEdge("payment", "fulfill")
	g.AddEdge("fulfill", "notify")

	if err := g.Compile(); err != nil {
		log.Fatalf("Compile: %v", err)
	}

	// 3. 创建引擎，用 Adapt 包装为 HTTP handler。
	engine := graph.NewEngine(g)
	handler := server.Adapt(engine, graph.WithTimeout(30*time.Second))

	// 4. 启动 HTTP 服务（工业级配置）。
	srv := server.NewHTTPServer(server.Config{
		Addr:           ":8080",
		MaxConcurrent:  100,               // 最大并发请求数
		MaxMessageSize: 1 << 20,           // 1 MiB
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		ShutdownTimeout: 10 * time.Second, // 优雅关闭等待时间
		Logger:         logger,
	})
	srv.Register("order-pipeline", handler)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("🚀 HTTP 服务已启动: http://localhost:8080")
	fmt.Println("   POST /order-pipeline  — 执行图")
	fmt.Println("   GET  /health          — 健康检查")
	fmt.Println("   GET  /metrics         — 运行指标 (JSON)")
	fmt.Println()
	fmt.Println("   测试:")
	fmt.Println(`   curl -X POST http://localhost:8080/order-pipeline -H 'Content-Type: application/json' -d '{"order_id":"ORD-001","amount":299.00}'`)
	fmt.Println(`   curl http://localhost:8080/metrics | jq`)

	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}

	slog.Info("server stopped")
}
