// 剪贴板写入 + 统一 toast（「<what>已复制」）。三处签名/凭据/邀请码复制
// 按钮共用；clipboard API 仅安全上下文可用，失败让异常冒给调用方的
// useApiAction/控制台（与既有各站点行为一致）。
import { toast } from 'vue-sonner'

export async function copyText(text: string, what: string): Promise<void> {
  await navigator.clipboard.writeText(text)
  toast.success(`${what}已复制`)
}
