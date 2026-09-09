// 搜索实现（architecture §13 语义：权限过滤 -> 结构化过滤 -> regex 过滤 ->
// fuzzy 排序 -> 分页）。
//
// 实现决策（TODO.md D7）：regex 用 Go regexp（RE2，线性时间，天然免疫
// ReDoS——架构文档担心的 POSIX regex 拖垮数据库问题在此路径上不存在）；
// fuzzy 用 Go 侧字符 trigram 相似度。候选集先按结构化过滤并封顶扫描量，
// 再在内存中过滤排序。工作区任务量到达万级或搜索出现延迟 SLO 前，
// 不引入 pg_trgm/POSIX SQL 路径；届时只需替换本文件实现，语义不变。
package task

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/modules/tag"
)

const (
	// maxRegexLen/maxFuzzyLen 与 openapi 参数约束一致。
	maxRegexLen = 512
	maxFuzzyLen = 256
	// maxScanCap 单次搜索最多载入内存的候选行数（结构化过滤之后）。
	maxScanCap = 2000
)

// ScoredTask 是搜索结果：任务 DTO + fuzzy 排序分（仅 fuzzy 查询时非 nil）。
type ScoredTask struct {
	Task  taskDTO
	Score *float64 `json:"score,omitempty"`
}

type SearchParams struct {
	Regex    string
	Fuzzy    string
	ParentID string
	Tag      string
	Status   string
	Limit    int
	Cursor   string // base64 候选集内偏移（候选集已封顶，内存分页稳定）
}

// Search 执行一次搜索并返回一页结果。调用方已完成 task:read 授权。
func (m *Module) Search(ctx context.Context, wsID string, params SearchParams) ([]ScoredTask, string, *httpx.APIError) {
	if len(params.Regex) > maxRegexLen {
		return nil, "", httpx.Invalid("regex too long")
	}
	if len(params.Fuzzy) > maxFuzzyLen {
		return nil, "", httpx.Invalid("fuzzy too long")
	}
	var re *regexp.Regexp
	if params.Regex != "" {
		var err error
		re, err = regexp.Compile(params.Regex)
		if err != nil {
			return nil, "", httpx.Invalid("invalid regex: " + err.Error())
		}
	}

	// 1. 结构化过滤（SQL）+ 候选集封顶。
	// 列名一律限定 tasks. 前缀：tag 过滤会 JOIN tags，后者拥有同名
	// workspace_id/created_at 列，避免歧义（sqlite/PG 均严格报错）。
	query := m.DB.WithContext(ctx).Model(&model.Task{}).Where("tasks.workspace_id = ?", wsID)
	if params.ParentID != "" {
		query = query.Where("tasks.parent_id = ?", params.ParentID)
	}
	if params.Status != "" {
		if !validStatus(params.Status) {
			return nil, "", httpx.Invalid("invalid status")
		}
		query = query.Where("tasks.status = ?", params.Status)
	}
	if params.Tag != "" {
		query = query.Joins("JOIN task_tags tt ON tt.task_id = tasks.id").
			Joins("JOIN tags g ON g.id = tt.tag_id").
			Where("g.normalized_name = ?", tag.NormalizeName(params.Tag))
	}
	var candidates []model.Task
	// JOIN 场景下列名需限定表名（tags 也有 created_at，避免歧义）。
	if err := query.Order("tasks.created_at DESC, tasks.id DESC").Limit(maxScanCap).Find(&candidates).Error; err != nil {
		return nil, "", httpx.Internal("search query failed")
	}

	// 2. regex 过滤（title + description）。
	pool := candidates
	if re != nil {
		filtered := pool[:0]
		for _, t := range pool {
			if re.MatchString(t.Title) || re.MatchString(t.Description) {
				filtered = append(filtered, t)
			}
		}
		pool = filtered
	}

	// 3. fuzzy 排序（title 权重高于 description）。
	var scores map[string]float64
	if params.Fuzzy != "" {
		scores = make(map[string]float64, len(pool))
		fuzzy := strings.ToLower(params.Fuzzy)
		for i := range pool {
			t := &pool[i]
			score := 0.85*trigramSimilarity(fuzzy, t.Title) + 0.15*trigramSimilarity(fuzzy, t.Description)
			scores[t.ID] = score
		}
		sort.SliceStable(pool, func(i, j int) bool { return scores[pool[i].ID] > scores[pool[j].ID] })
	}

	// 4. 内存分页（候选集已封顶，偏移稳定）。
	offset := 0
	if params.Cursor != "" {
		var err error
		offset, err = decodeSearchCursor(params.Cursor)
		if err != nil || offset < 0 {
			return nil, "", httpx.Invalid("invalid cursor")
		}
		if offset > len(pool) {
			offset = len(pool)
		}
	}
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	end := offset + limit
	if end > len(pool) {
		end = len(pool)
	}
	next := ""
	if end < len(pool) {
		next = encodeSearchCursor(end)
	}

	out := make([]ScoredTask, 0, end-offset)
	for i := offset; i < end; i++ {
		st := ScoredTask{Task: toTaskDTO(pool[i], nil, nil)}
		if scores != nil {
			v := scores[pool[i].ID]
			st.Score = &v
		}
		out = append(out, st)
	}
	return out, next, nil
}

// trigramSimilarity 计算 a 与 b 的字符 trigram Jaccard 相似度（0~1）。
// 与 pg_trgm 的相似度语义近似（排序用途足够；不追求逐位一致）。
func trigramSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	pad := func(s string) string { return "  " + s + "  " }
	set := func(s string) map[string]struct{} {
		m := make(map[string]struct{}, utf8.RuneCountInString(s))
		rs := []rune(s)
		for i := 0; i+3 <= len(rs); i++ {
			m[string(rs[i:i+3])] = struct{}{}
		}
		return m
	}
	ga, gb := set(pad(strings.ToLower(a))), set(pad(strings.ToLower(b)))
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	inter := 0
	for g := range ga {
		if _, ok := gb[g]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(ga)+len(gb)-inter)
}

func encodeSearchCursor(offset int) string {
	raw, _ := json.Marshal(offset)
	return string(raw) // 短数字字符串，直接可读；不透明性由 next_cursor 契约保证
}

func decodeSearchCursor(s string) (int, error) {
	var offset int
	if err := json.Unmarshal([]byte(s), &offset); err != nil {
		return 0, err
	}
	return offset, nil
}
