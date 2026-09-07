// Package migrations 以 embed 方式把 goose SQL 随二进制分发，
// 保证运行中的二进制与它执行的 schema 永远同版本。
// 也可以用 goose CLI 直接对本目录执行 `goose up`（运维路径见 docs/deployment.md §14）。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
