package httpx

import (
	"net/http"
	"strconv"
)

// MinCLIProtocolVersion 是服务端当前接受的最小客户端协议大版本。
// 与 GET /.well-known/astral 的 min_cli_protocol_version 字段同源：
// well-known 处理器应引用本常量，避免两处硬编码漂移（R2 裁决）。
const MinCLIProtocolVersion = 2

// ClientVersionMiddleware 实施客户端协议版本协商下限（R2 / NFR-004）：
// 请求携带 X-Astral-Client-Version 头时，值必须为不小于
// MinCLIProtocolVersion 的正整数，否则整个请求被 400
// CLIENT_VERSION_UNSUPPORTED 拒绝；头缺失一律放行（浏览器与探测请求
// 不破，契约是"支持协商"而非"要求携带"）。由 app 层挂载在 /api/v1
// 之下，全方法生效；客户端应先读 /.well-known 自查版本再发请求
// （docs/protocol.md §7）。
func ClientVersionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(HeaderClientVersion)
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < MinCLIProtocolVersion {
			WriteError(w, r, &APIError{
				Status: http.StatusBadRequest,
				Code:   CodeClientVersionUnsupported,
				Message: "client protocol version unsupported: " + raw +
					" (minimum accepted is " + strconv.Itoa(MinCLIProtocolVersion) +
					"; see /.well-known/astral min_cli_protocol_version)",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
