// tcp_server 演示如何将图引擎暴露为 TCP 服务（NDJSON 协议）。
//
//  用法:
//    go run ./examples/04_tcp_server
//
//  测试:
//    echo '{"graph":"order-pipeline","state":{"order_id":"ORD-001","amount":299.00}}' | nc localhost 9090
//
//    echo '{"graph":"order-pipeline","state":{"order_id":"ORD-002","amount":99999.00}}' | nc localhost 9090
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
	log.Printf("[validate] 校验订单 %s...", s.OrderID)
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
	log.Printf("[payment] 处理支付 %.2f...", s.Amount)
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
	log.Printf("[fulfill] 发货中...")
	time.Sleep(150 * time.Millisecond)
	s.Fulfilled = true
	s.Steps = append(s.Steps, "fulfilled")
	return s, nil
}

func cancelOrder(ctx context.Context, s OrderState) (OrderState, error) {
	log.Printf("[cancel] 取消订单 %s...", s.OrderID)
	time.Sleep(50 * time.Millisecond)
	s.Steps = append(s.Steps, "cancelled")
	return s, nil
}

func sendNotification(ctx context.Context, s OrderState) (OrderState, error) {
	log.Printf("[notify] 发送通知...")
	time.Sleep(80 * time.Millisecond)
	s.Notified = true
	s.Steps = append(s.Steps, "notified")
	return s, nil
}

func isPaymentFailed(ctx context.Context, s OrderState) bool {
	return s.Error != "" && !s.Paid
}

func main() {
	// 1. 构建图。
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

	// 2. 设置结构化日志。
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 3. 创建引擎并包装为 TCP handler。
	engine := graph.NewEngine(g)
	handler := server.Adapt(engine, graph.WithTimeout(30*time.Second))

	// 4. 启动 TCP 服务。
	srv := server.NewTCPServer(server.Config{
		Addr:           ":9090",
		MaxMessageSize: 1 << 20,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		Logger:         logger,
	})
	srv.Register("order-pipeline", handler)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("🔌 TCP 服务已启动: localhost:9090")
	fmt.Println("   协议: 换行分隔 JSON (NDJSON)")
	fmt.Println()
	fmt.Println("   测试:")
	fmt.Println(`   echo '{"graph":"order-pipeline","state":{"order_id":"ORD-001","amount":299.00}}' | nc localhost 9090`)

	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("TCP server: %v", err)
	}
}
