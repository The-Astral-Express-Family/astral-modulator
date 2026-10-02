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

// TrimPage 截断游标分页的探针行：查询以 limit+1 多取一行探测「还有下一页」，
// 有则截去探针并以本页末行的游标值（cursorOf 提取）作为 next 游标；不足一页
// next 为空串（分页 envelope 的尾页语义）。各集合端点的探针截断必须经此，
// 与 ParseLimit 同为分页单点（游标列因集合而异，故提取器作参数）。
func TrimPage[T any](rows []T, limit int, cursorOf func(T) string) ([]T, string) {
	if len(rows) <= limit {
		return rows, ""
	}
	rows = rows[:limit]
	return rows, cursorOf(rows[len(rows)-1])
}
