package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ids"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
)

// ---- Device Authorization Grant（RFC 8628 简化版；A1 语义见 TODO.md）----
//
//	create   → pending 行（device_code hash 落库）
//	approve  → 浏览器侧（human session）把行置 approved + actor
//	exchange → CLI 轮询：pending→AUTHORIZATION_PENDING(400)；
//	           过快→SLOW_DOWN(400)；denied→401；expired→401；approved→一次性发放并置 exchanged

type DeviceAuthorizationCreated struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceAuthRow = model.DeviceAuthorization

// CreateDeviceAuthorization 生成待审批的 device 授权请求。
// publicURL 用于拼 verification_uri（web 路由 /device 固定）。
func (s *Service) CreateDeviceAuthorization(ctx context.Context, clientType, publicURL string) (*DeviceAuthorizationCreated, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	if clientType != "cli" && clientType != "web" {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "client_type must be cli or web"}
	}
	deviceCode, err := NewDeviceCode()
	if err != nil {
		return nil, err
	}
	userCode, err := NewUserCode()
	if err != nil {
		return nil, err
	}
	row := &deviceAuthRow{
		ID:             ids.New(ids.Device),
		DeviceCodeHash: HashToken(deviceCode),
		UserCode:       userCode,
		ClientType:     clientType,
		Status:         "pending",
		ExpiresAt:      time.Now().Add(s.DeviceTTL),
	}
	if err := s.DB.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	base := strings.TrimRight(publicURL, "/")
	return &DeviceAuthorizationCreated{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         base + "/device",
		VerificationURIComplete: base + "/device?code=" + userCode,
		ExpiresIn:               int(s.DeviceTTL.Seconds()),
		Interval:                int(s.DevicePollEvery.Seconds()),
	}, nil
}

// DeviceAuthorizationView 是审批页可见的脱敏视图（不含 device_code hash）。
type DeviceAuthorizationView struct {
	ID         string `json:"id"`
	UserCode   string `json:"user_code"`
	ClientType string `json:"client_type"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
}

// FindByUserCode 供审批页查询（A3）。只返回未过期的行；过期行惰性置 expired。
func (s *Service) FindByUserCode(ctx context.Context, userCode string) (*DeviceAuthorizationView, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	userCode = strings.ToUpper(strings.TrimSpace(userCode))
	var row deviceAuthRow
	err := s.DB.WithContext(ctx).Where("user_code = ?", userCode).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "unknown user_code"}
	}
	if err != nil {
		return nil, err
	}
	s.expireIfDue(ctx, &row)
	return &DeviceAuthorizationView{
		ID: row.ID, UserCode: row.UserCode, ClientType: row.ClientType,
		Status: row.Status, CreatedAt: row.CreatedAt.Format(time.RFC3339), ExpiresAt: row.ExpiresAt.Format(time.RFC3339),
	}, nil
}

// Approve / Deny：浏览器侧人工裁决（需 human session，由 handler 层校验）。
func (s *Service) Approve(ctx context.Context, authorizationID, actorID string) error {
	return s.decide(ctx, authorizationID, actorID, "approved")
}

func (s *Service) Deny(ctx context.Context, authorizationID, actorID string) error {
	return s.decide(ctx, authorizationID, actorID, "denied")
}

func (s *Service) decide(ctx context.Context, authorizationID, actorID, status string) error {
	res := s.DB.WithContext(ctx).Model(&deviceAuthRow{}).
		Where("id = ? AND status = 'pending' AND expires_at > ?", authorizationID, time.Now()).
		Updates(map[string]any{"status": status, "actor_id": actorID})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &httpx.APIError{Status: 409, Code: httpx.CodeValidationFailed, Message: "authorization not pending or expired"}
	}
	return nil
}

func (s *Service) expireIfDue(ctx context.Context, row *deviceAuthRow) {
	if row.Status == "pending" && time.Now().After(row.ExpiresAt) {
		_ = s.DB.WithContext(ctx).Model(&deviceAuthRow{}).Where("id = ?", row.ID).Update("status", "expired").Error
		row.Status = "expired"
	}
}

// ExchangeDeviceToken 用 device_code 兑换 token（单次）。
func (s *Service) ExchangeDeviceToken(ctx context.Context, deviceCode, ip, ua string) (*TokenPair, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	if deviceCode == "" {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "missing device_code"}
	}
	var row deviceAuthRow
	err := s.DB.WithContext(ctx).Where("device_code_hash = ?", HashToken(deviceCode)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "unknown device_code"}
	}
	if err != nil {
		return nil, err
	}
	s.expireIfDue(ctx, &row)

	switch row.Status {
	case "pending":
		// A1：过快轮询惩罚。CreatedAt==UpdatedAt 表示尚无轮询记录，首次轮询不罚。
		if !row.UpdatedAt.Equal(row.CreatedAt) && time.Since(row.UpdatedAt) < s.DevicePollEvery/2 {
			return nil, &httpx.APIError{Status: 400, Code: httpx.CodeSlowDown, Message: "polling too fast; back off", Retryable: boolPtr(true)}
		}
		// 触碰 updated_at 作为 last_poll 记录。
		_ = s.DB.WithContext(ctx).Model(&deviceAuthRow{}).Where("id = ?", row.ID).Update("updated_at", time.Now()).Error
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeAuthorizationPending, Message: "authorization pending", Retryable: boolPtr(true)}
	case "denied":
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "authorization denied"}
	case "expired":
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenExpired, Message: "device authorization expired"}
	case "exchanged":
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "device_code already used"}
	}

	if row.ActorID == nil {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeAuthRequired, Message: "authorization not approved"}
	}

	// approved → 单次兑换：原子置 exchanged，抢不到说明并发已兑换。
	res := s.DB.WithContext(ctx).Model(&deviceAuthRow{}).
		Where("id = ? AND status = 'approved'", row.ID).
		Updates(map[string]any{"status": "exchanged", "exchanged_at": time.Now()})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, &httpx.APIError{Status: 401, Code: httpx.CodeTokenRevoked, Message: "device_code already used"}
	}

	refresh, access, _, err := s.createSession(ctx, *row.ActorID, "cli", ip, ua)
	if err != nil {
		return nil, err
	}

	var actor model.Actor
	if err := s.DB.WithContext(ctx).First(&actor, "id = ?", *row.ActorID).Error; err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.AccessTTL.Seconds()),
		RefreshToken: refresh,
		ActorID:      actor.ID,
	}, nil
}

func boolPtr(b bool) *bool { return &b }

// ---- Agent / Service Credential ----

type CreateCredentialInput struct {
	ActorID   string
	Kind      string // agent | service
	Scopes    []string
	Workspace *string // 绑定 workspace（可空）
	CreatedBy string
	ExpiresAt *time.Time
}

type IssuedCredential struct {
	CredentialID string   `json:"credential_id"`
	Secret       string   `json:"secret"`
	Scopes       []string `json:"scopes"`
	ExpiresAt    *string  `json:"expires_at"`
}

// IssueCredential 明文 secret 只出现一次（architecture §9）。
func (s *Service) IssueCredential(ctx context.Context, in CreateCredentialInput) (*IssuedCredential, error) {
	if _, dbErr := s.dbOrError(); dbErr != nil {
		return nil, dbErr
	}
	if in.Kind != "agent" && in.Kind != "service" {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "kind must be agent or service"}
	}
	if len(in.Scopes) == 0 {
		return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "scopes required"}
	}
	known := map[string]bool{}
	for _, sc := range AllScopes {
		known[sc] = true
	}
	for _, sc := range in.Scopes {
		if !known[sc] {
			return nil, &httpx.APIError{Status: 400, Code: httpx.CodeValidationFailed, Message: "unknown scope: " + sc}
		}
	}
	secret, err := NewCredentialSecret()
	if err != nil {
		return nil, err
	}
	scopesJSON, _ := json.Marshal(in.Scopes)
	cred := &model.Credential{
		ID:          ids.New(ids.Credential),
		ActorID:     in.ActorID,
		Kind:        in.Kind,
		SecretHash:  HashToken(secret),
		WorkspaceID: in.Workspace,
		Scopes:      string(scopesJSON),
		CreatedBy:   in.CreatedBy,
		ExpiresAt:   in.ExpiresAt,
	}
	if err := s.DB.WithContext(ctx).Create(cred).Error; err != nil {
		return nil, err
	}
	var exp *string
	if in.ExpiresAt != nil {
		f := in.ExpiresAt.Format(time.RFC3339)
		exp = &f
	}
	return &IssuedCredential{CredentialID: cred.ID, Secret: secret, Scopes: in.Scopes, ExpiresAt: exp}, nil
}

// RevokeCredential 立即吊销。
func (s *Service) RevokeCredential(ctx context.Context, credentialID string) error {
	res := s.DB.WithContext(ctx).Model(&model.Credential{}).
		Where("id = ? AND revoked_at IS NULL", credentialID).Update("revoked_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &httpx.APIError{Status: 404, Code: httpx.CodeValidationFailed, Message: "credential not found or already revoked"}
	}
	return nil
}
