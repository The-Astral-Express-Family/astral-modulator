// Package background 统一进程内周期任务的 goroutine 形状：
// ctx 取消即退出、ticker 资源释放、单次执行不外泄 panic。
// 各模块不再自写 select-ticker 循环。
package background

import (
	"context"
	"time"
)

// RunEvery 以固定间隔执行 fn，直到 ctx 取消。fn 的错误处理（记日志/跳过）
// 由调用方在闭包内完成；fn panic 会被吞掉（周期任务不应拖垮进程）。
func RunEvery(ctx context.Context, every time.Duration, fn func(ctx context.Context)) {
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				func() {
					defer func() { _ = recover() }()
					fn(ctx)
				}()
			}
		}
	}()
}
