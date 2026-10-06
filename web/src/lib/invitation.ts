// 邀请签发面的共享展示件。TTL 档位与 auth.ParseInviteTTL 的边界对应
//（1s~30d；三档是 UI 快捷项，非全集——expires_in 由服务端钳制）。

/** 邀请有效期快捷档（秒 → 中文标签）；工作区邀请与注册邀请两处签发面共用。 */
export const TTL_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: '86400', label: '1 天' },
  { value: '604800', label: '7 天' },
  { value: '2592000', label: '30 天' },
]
