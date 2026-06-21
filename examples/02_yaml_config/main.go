// yaml_config 演示使用 YAML 配置文件驱动图引擎，而非硬编码构建图。
//
//  用法:
//    go run ./examples/02_yaml_config
//
// 场景: 与 workflow 示例相同的订单处理管线，但图结构完全由 workflow.yaml 配置。
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wzhongyou/weave/graph"
)

// OrderState 订单状态（与 workflow 示例相同）。
type OrderState struct {
	OrderID   string  `json:"order_id"`
	Amount    float64 `json:"amount"`
	Validated bool    `json:"validated"`
	Paid      bool    `json:"paid"`
	Fulfilled bool    `json:"fulfilled"`
	Notified  bool    `json:"notified"`
	Error     string  `json:"error"`
	Steps     []string `json:"steps"`
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
	if s.Amount > 10000 {
		s.Error = "余额不足"
		s.Steps = append(s.Steps, "payment_failed")
		fmt.Println("  ✗ 支付失败: 余额不足")
		// 不返回 error，让条件路由根据 state 决定走向 cancel。
		return s, nil
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

// paymentCheck 条件函数：返回 "failed" 进入取消流程，否则走默认成功路径。
func paymentCheck(ctx context.Context, s OrderState) string {
	if s.Error != "" && !s.Paid {
		return "failed"
	}
	return "ok"
}

func main() {
	// 1. 创建注册表，将 YAML 中的 type 名称映射到 Go 实现。
	reg := graph.NewRegistry[OrderState]()
	reg.RegisterNode("validate", validateOrder)
	reg.RegisterNode("payment", processPayment)
	reg.RegisterNode("fulfill", fulfillOrder)
	reg.RegisterNode("cancel", cancelOrder)
	reg.RegisterNode("notify", sendNotification)
	reg.RegisterCondition("payment_check", paymentCheck)

	// 2. 从 YAML 配置文件加载图。
	g, err := graph.LoadFromFile("examples/02_yaml_config/workflow.yaml", reg)
	if err != nil {
		panic(fmt.Sprintf("加载配置失败: %v", err))
	}

	// 3. 创建引擎并执行。
	engine := graph.NewEngine(g)

	// 场景 1: 正常订单
	fmt.Println("═══ 场景 1: 正常订单 ═══")
	result, err := engine.Run(context.Background(), OrderState{
		OrderID: "ORD-001",
		Amount:  299.00,
	})
	printResult("正常订单", result, err)

	// 场景 2: 大额订单（支付失败）
	fmt.Println("\n═══ 场景 2: 大额订单（支付失败）═══")
	result2, err := engine.Run(context.Background(), OrderState{
		OrderID: "ORD-002",
		Amount:  99999.00,
	})
	printResult("大额订单", result2, err)
}

func printResult(name string, result *graph.ExecutionResult[OrderState], err error) {
	if err != nil {
		fmt.Printf("[%s] 错误: %v\n", name, err)
		if result != nil {
			fmt.Printf("  执行步骤: %v\n", result.FinalState.Steps)
			fmt.Printf("  耗时: %v\n", result.TotalDuration.Round(time.Millisecond))
		}
	} else {
		fmt.Printf("[%s] 完成\n", name)
		fmt.Printf("  执行步骤: %v\n", result.FinalState.Steps)
		fmt.Printf("  节点执行: %d\n", result.NodeCount)
		fmt.Printf("  总步数: %d\n", result.TotalSteps)
		fmt.Printf("  耗时: %v\n", result.TotalDuration.Round(time.Millisecond))
	}
}
