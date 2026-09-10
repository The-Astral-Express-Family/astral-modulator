// filters.go：children 集合（children.go）与 task-search（search.go）共用的
// 结构化过滤。两条查询路径必须同一语义，本文件是过滤条件的唯一事实来源——
// 改过滤行为只改这里，禁止在调用方各自手写。
package task

import (
	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
)

// taskFilters 结构化过滤三件套（children 与 search 的公共子集）。
// 字段均可选，空串 = 不过滤。
type taskFilters struct {
	Status   string
	Tag      string
	Assignee string
}

// applyTaskFilters 把 status/tag/assignee 过滤加到查询上；status 只在此校验
// （非法 → 400）。列名一律限定 tasks. 前缀：tag 过滤会 JOIN tags，后者也拥有
// workspace_id 等同名列，不限定会在 sqlite/PG 下歧义报错（调用方的基础条件
// 同样遵守此前缀约定）。
func applyTaskFilters(query *gorm.DB, f taskFilters) (*gorm.DB, *httpx.APIError) {
	if f.Status != "" {
		if !validStatus(f.Status) {
			return nil, httpx.Invalid("invalid status")
		}
		query = query.Where("tasks.status = ?", f.Status)
	}
	if f.Tag != "" {
		query = query.Joins("JOIN task_tags tt ON tt.task_id = tasks.id").
			Joins("JOIN tags g ON g.id = tt.tag_id").
			Where("g.normalized_name = ?", tag.NormalizeName(f.Tag))
	}
	if f.Assignee != "" {
		query = query.Where("tasks.assignee_actor_id = ?", f.Assignee)
	}
	return query, nil
}
