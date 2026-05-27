// Package billing 实现网关计费：定价表、三阶段配额消费、用量记录。
package billing

import "github.com/kingford/TopoLLM/internal/config"

// pricePerUnit 价格以"每百万 token"计。
const pricePerUnit = 1_000_000.0

// PriceTable 按模型提供 input/output 单价。未命中模型回退到 "default" 键（零值=免费）。
type PriceTable struct {
	prices map[string]config.Price
}

// NewPriceTable 构建价格表。
func NewPriceTable(prices map[string]config.Price) *PriceTable {
	if prices == nil {
		prices = map[string]config.Price{}
	}
	return &PriceTable{prices: prices}
}

func (pt *PriceTable) priceFor(model string) config.Price {
	if p, ok := pt.prices[model]; ok {
		return p
	}
	return pt.prices["default"]
}

// Cost 按实际用量计算费用。
func (pt *PriceTable) Cost(model string, promptTokens, completionTokens int) float64 {
	p := pt.priceFor(model)
	return float64(promptTokens)/pricePerUnit*p.Input + float64(completionTokens)/pricePerUnit*p.Output
}

// Estimate 在请求前估算费用（prompt 估计值按 input 计，期望最大输出按 output 计）。
func (pt *PriceTable) Estimate(model string, promptTokens, maxTokens int) float64 {
	return pt.Cost(model, promptTokens, maxTokens)
}
