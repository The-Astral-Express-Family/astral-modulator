package httpx

import "strconv"

// ParseLimit 是集合端点 limit 查询参数的统一钳制：空/非法/<1 回落 def，
// 超上限截到 max。openapi parameters/Limit（default 50 / maximum 200）的
// 服务端单点实现——各集合端点的分页行为必须经此，不得自写解析。
func ParseLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
