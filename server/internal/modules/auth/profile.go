// actor 资料域（00012_actor_profile）：snake_case DTO + PATCH /auth/me 的业务。
// DTO 单独存在的原因：model.Actor 无 json tag，直接序列化会漏出 PascalCase
// 字段名，与 openapi/前端的 snake_case 契约漂移（workspace 模块同款做法）。

package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// actorDTO 是对外暴露的 actor 形状（openapi Actor schema）。
// bio/avatar_url 恒为 string，空串 = 未设置。
type actorDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	Bio         string `json:"bio"`
	AvatarURL   string `json:"avatar_url"`
}

func newActorDTO(a model.Actor) actorDTO {
	url := ""
	if a.AvatarURL != nil {
		url = *a.AvatarURL
	}
	return actorDTO{ID: a.ID, Kind: a.Kind, DisplayName: a.DisplayName, Bio: a.Bio, AvatarURL: url}
}

// UpdateProfileInput 是 PATCH /auth/me 的请求体；nil 字段 = 不修改。
type UpdateProfileInput struct {
	DisplayName *string `json:"display_name"`
	Bio         *string `json:"bio"`
	AvatarURL   *string `json:"avatar_url"`
}

const (
	maxDisplayNameLen = 200
	maxBioLen         = 500
	maxAvatarURLLen   = 500
)

// UpdateProfile 更新自己的 actor 资料。任意已认证 principal（human session /
// agent credential）可改自己；display_name 唯一强制项（非空）。
func (s *Service) UpdateProfile(ctx context.Context, actorID string, in UpdateProfileInput) (*model.Actor, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	updates := map[string]any{}
	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if name == "" {
			return nil, httpx.Invalid("display_name required")
		}
		if utf8.RuneCountInString(name) > maxDisplayNameLen {
			return nil, httpx.Invalid("display_name too long")
		}
		updates["display_name"] = name
	}
	if in.Bio != nil {
		bio := strings.TrimSpace(*in.Bio)
		if utf8.RuneCountInString(bio) > maxBioLen {
			return nil, httpx.Invalid("bio too long")
		}
		updates["bio"] = bio
	}
	if in.AvatarURL != nil {
		avatar := strings.TrimSpace(*in.AvatarURL)
		// 空串 = 清除头像。仅收 http(s) 外链；服务端不抓取该 URL（无 SSRF 面）。
		if avatar != "" {
			if len(avatar) > maxAvatarURLLen {
				return nil, httpx.Invalid("avatar_url too long")
			}
			u, err := url.Parse(avatar)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, httpx.Invalid("avatar_url must be an http(s) URL")
			}
		}
		if avatar == "" {
			updates["avatar_url"] = nil
		} else {
			updates["avatar_url"] = avatar
		}
	}
	if len(updates) == 0 {
		// 全字段缺省：幂等 no-op，直接返回当前资料。
		var actor model.Actor
		if err := s.DB.WithContext(ctx).First(&actor, "id = ?", actorID).Error; err != nil {
			return nil, err
		}
		return &actor, nil
	}
	res := s.DB.WithContext(ctx).Model(&model.Actor{}).Where("id = ?", actorID).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, httpx.NotFound("actor not found")
	}
	var actor model.Actor
	if err := s.DB.WithContext(ctx).First(&actor, "id = ?", actorID).Error; err != nil {
		return nil, err
	}
	return &actor, nil
}

// HumanEmail 返回 human actor 的本地登录邮箱（human_auth）；非 human 或无记录
// 返回空串。Me 响应用它展示只读邮箱，查询失败不致命（省略即可）。
func (s *Service) HumanEmail(ctx context.Context, actorID string) (string, error) {
	var ha model.HumanAuth
	err := s.DB.WithContext(ctx).Select("email").Where("actor_id = ?", actorID).First(&ha).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ha.Email, nil
}
