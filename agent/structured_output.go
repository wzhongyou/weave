package agent

import (
	"encoding/json"
	"fmt"
)

// StructuredOutputConfig 配置结构化输出（JSON Schema 约束的 LLM 输出）。
type StructuredOutputConfig struct {
	// Schema 是一个 JSON Schema 文档，描述期望的输出结构。
	Schema map[string]any
	// Instruction 是 system prompt 中附加指令的覆盖值。
	// 默认值: "你必须返回符合以下 JSON Schema 的合法 JSON：{schema}"
	Instruction string
	// SchemaName 是 Schema 的名称，用于描述输出用途。
	SchemaName string
}

// BuildInstruction 生成附加到 system prompt 的结构化输出指令。
func (c *StructuredOutputConfig) BuildInstruction() string {
	if c.Instruction != "" {
		return c.Instruction
	}
	schemaJSON, err := json.Marshal(c.Schema)
	if err != nil {
		return "你必须返回合法 JSON。"
	}
	name := c.SchemaName
	if name == "" {
		name = "输出"
	}
	return fmt.Sprintf("你必须返回符合以下 JSON Schema 的合法 JSON（%s）：\n```json\n%s\n```\n不要添加任何其他文字或 Markdown 格式。",
		name, string(schemaJSON))
}

// ValidateStructuredOutput 校验内容是否符合 JSON Schema。
// 返回解析后的 JSON 对象，如果校验失败则返回错误。
func ValidateStructuredOutput(content string, schema map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("结构化输出解析失败（不是合法 JSON）: %w", err)
	}

	// 校验必需字段是否存在
	if requiredRaw, ok := schema["required"]; ok {
		switch required := requiredRaw.(type) {
		case []any:
			for _, field := range required {
				fieldName, ok := field.(string)
				if !ok {
					continue
				}
				if _, exists := result[fieldName]; !exists {
					return nil, fmt.Errorf("结构化输出校验失败: 缺少必需字段 %q", fieldName)
				}
			}
		case []string:
			for _, fieldName := range required {
				if _, exists := result[fieldName]; !exists {
					return nil, fmt.Errorf("结构化输出校验失败: 缺少必需字段 %q", fieldName)
				}
			}
		}
	}

	return result, nil
}
