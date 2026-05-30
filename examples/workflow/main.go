// workflow_demo 展示纯图引擎的业务工作流编排，完全不涉及 AI。
//
//  用法:
//    go run ./examples/workflow
//
// 场景: 订单处理管线 — validate → [支付成功?] → fulfill → notify
//                                          ↘ [失败] → cancel
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wzhongyou/graphflow/graph"
	"github.com/wzhongyou/graphflow/graph/checkpoint"
)

// ── 订单状态 ────────────────────────────────────────────────────────────────

type OrderState struct {
	OrderID    string
	Amount     float64
	Validated  bool
	Paid       bool
	Fulfilled  bool
	Notified   bool
	Error      string
	Steps      []string
}

// ── 节点函数 ────────────────────────────────────────────────────────────────

func validateOrder(ctx context.Context, s OrderState) (OrderState, error) {
	fmt.Printf("[validate] 校验订单 %s (金额: %.2f)...\n", s.OrderID, s.Amount)
	time.Sleep(100 * time.Millisecond)

	if s.Amount <= 0 {
		s.Error = "订单金额无效"
		return s, fmt.Errorf("订单金额无效: %.2f", s.Amount)
	}
	s.Validated = true
	s.Steps = append(s.Steps, "validated")
	fmt.Println("  ✓ 校验通过")
	return s, nil
}

func processPayment(ctx context.Context, s OrderState) (OrderState, error) {
	fmt.Printf("[payment] 处理支付 %.2f...\n", s.Amount)
	time.Sleep(200 * time.Millisecond)

	// 模拟：金额 > 10000 时支付失败
	if s.Amount > 10000 {
		s.Error = "余额不足"
		s.Steps = append(s.Steps, "payment_failed")
		fmt.Println("  ✗ 支付失败: 余额不足")
		return s, fmt.Errorf("支付失败: 余额不足")
	}

	s.Paid = true
	s.Steps = append(s.Steps, "paid")
	fmt.Println("  ✓ 支付成功")
	return s, nil
}

func fulfillOrder(ctx context.Context, s OrderState) (OrderState, error) {
	fmt.Printf("[fulfill] 发货中...\n")
	time.Sleep(150 * time.Millisecond)
	s.Fulfilled = true
	s.Steps = append(s.Steps, "fulfilled")
	fmt.Println("  ✓ 已发货")
	return s, nil
}

func cancelOrder(ctx context.Context, s OrderState) (OrderState, error) {
	fmt.Printf("[cancel] 取消订单 %s...\n", s.OrderID)
	time.Sleep(50 * time.Millisecond)
	s.Steps = append(s.Steps, "cancelled")
	fmt.Printf("  ✓ 订单已取消 (原因: %s)\n", s.Error)
	return s, nil
}

func sendNotification(ctx context.Context, s OrderState) (OrderState, error) {
	fmt.Printf("[notify] 发送通知...\n")
	time.Sleep(80 * time.Millisecond)
	s.Notified = true
	s.Steps = append(s.Steps, "notified")
	fmt.Println("  ✓ 通知已发送")
	return s, nil
}

// ── 条件函数 ────────────────────────────────────────────────────────────────

func isPaymentFailed(ctx context.Context, s OrderState) bool {
	return s.Error != "" && !s.Paid
}

// ── Main ────────────────────────────────────────────────────────────────────

func main() {
	// ── 1. 构建图 ──────────────────────────────────────────────────────
	g := graph.NewGraph[OrderState]("order-pipeline")

	g.AddNode("validate", validateOrder)
	g.AddNode("payment", processPayment)
	g.AddNode("fulfill", fulfillOrder)
	g.AddNode("cancel", cancelOrder)
	g.AddNode("notify", sendNotification)

	g.SetEntryPoint("validate")
	g.AddEdge("validate", "payment")

	// 条件路由：支付失败 → cancel，成功 → fulfill
	g.AddCondition("payment", graph.Condition[OrderState]{
		If:     isPaymentFailed,
		Target: "cancel",
	})
	// 无条件边作为 fallback（支付成功时的路径）
	g.AddEdge("payment", "fulfill")

	g.AddEdge("fulfill", "notify")
	g.Compile()

	// ── 2. 运行业务场景 ────────────────────────────────────────────────
	engine := graph.NewEngine(g)

	// 场景 1: 正常订单
	fmt.Println("═══ 场景 1: 正常订单 ═══")
	result1, err1 := engine.Run(context.Background(), OrderState{
		OrderID: "ORD-001",
		Amount:  299.00,
	})
	printResult("正常订单", result1, err1)

	// 场景 2: 大额订单（支付失败）
	fmt.Println("\n═══ 场景 2: 大额订单（支付失败）═══")
	result2, err2 := engine.Run(context.Background(), OrderState{
		OrderID: "ORD-002",
		Amount:  99999.00,
	})
	printResult("大额订单", result2, err2)

	// 场景 3: 带 Checkpoint 的订单
	fmt.Println("\n═══ 场景 3: 带 Checkpoint ═══")
	cpManager := checkpoint.NewFileManager("/tmp/graphflow_checkpoints")

	result3, err3 := engine.Run(context.Background(), OrderState{
		OrderID: "ORD-003",
		Amount:  59.90,
	}, graph.WithCheckpoint(cpManager), graph.WithTimeout(10*time.Second))
	printResult("带 Checkpoint", result3, err3)
}

func printResult(name string, result *graph.ExecutionResult[OrderState], err error) {
	if err != nil {
		fmt.Printf("[%s] 错误: %v\n", name, err)
		fmt.Printf("  执行步骤: %v\n", result.FinalState.Steps)
		fmt.Printf("  耗时: %v\n", result.TotalDuration.Round(time.Millisecond))
	} else {
		fmt.Printf("[%s] 完成\n", name)
		fmt.Printf("  执行步骤: %v\n", result.FinalState.Steps)
		fmt.Printf("  节点执行: %d\n", result.NodeCount)
		fmt.Printf("  总步数: %d\n", result.TotalSteps)
		fmt.Printf("  耗时: %v\n", result.TotalDuration.Round(time.Millisecond))
	}
}
