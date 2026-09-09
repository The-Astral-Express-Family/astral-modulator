// Package ptr 提供值到指针的通用转换，
// 消除各模块 `v := x; field = &v` 的手工取址样板。
package ptr

// Of 返回 v 的指针。每次调用分配新副本。
func Of[T any](v T) *T { return &v }
