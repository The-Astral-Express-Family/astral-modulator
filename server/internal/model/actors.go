// actor 展示信息的共享查询：多个列表端点（workspace 成员 / presence 等）
// 都是「拿一组 actorID → 一次 IN 查询 → 按 ID 建索引」，收敛到单点避免
// 各自手写并悄悄漂移。
package model

import (
	"context"

	"gorm.io/gorm"
)

// ActorsByIDs 一次 IN 查询取回 actors，按 ID 建索引（缺行 = map 无键，由
// 调用方决定呈现语义）。ids 允许含重复与空集（空集直接返回空 map，不查库）。
// 需要额外过滤（kind、排序、列裁剪）的调用方自行查库，不走本 helper。
func ActorsByIDs(ctx context.Context, db *gorm.DB, ids []string) (map[string]Actor, error) {
	byID := make(map[string]Actor, len(ids))
	if len(ids) == 0 {
		return byID, nil
	}
	var actors []Actor
	if err := db.WithContext(ctx).Where("id IN ?", ids).Find(&actors).Error; err != nil {
		return nil, err
	}
	for _, a := range actors {
		byID[a.ID] = a
	}
	return byID, nil
}
