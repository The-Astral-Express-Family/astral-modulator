// astral-bootstrap 是 astral-server 的交互式初始化向导入口。
// 流程编排与提示原语在 internal/bootstrap；本文件保持极薄。
// 用法：cd server && go run ./cmd/astral-bootstrap
package main

import (
	"fmt"
	"os"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/bootstrap"
)

func main() {
	if err := bootstrap.Run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "astral-bootstrap:", err)
		os.Exit(1)
	}
}
