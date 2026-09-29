// 兑码入伙共用流程（ADR-0009）：对话框（侧栏入口）与 /join 深链视图共用。
// 成功/幂等 → 刷新工作区列表并跳转该工作区；已是成员（ALREADY_MEMBER，
// 全局 toast 已提示）→ 刷新列表跳回主页让用户从切换器进入；其余失败
// （INVITE_INVALID 等）全局 toast 已提示，调用方留在原地可改码重试。
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { isApiErrorCode } from '@/api/client'
import { redeemInvitation } from '@/api/modules/workspace'
import { useWorkspacesStore } from '@/stores/workspaces'

const ROLE_LABELS: Record<string, string> = {
  viewer: '观察者',
  contributor: '贡献者',
  maintainer: '维护者',
  owner: '所有者',
  agent: 'agent',
}

export function useJoinWorkspace() {
  const router = useRouter()
  const workspaces = useWorkspacesStore()
  const joining = ref(false)

  /** 返回 true = 旅程已落定（成功/幂等/已是成员），调用方可收起表单或跳走。 */
  async function join(rawCode: string): Promise<boolean> {
    const code = rawCode.trim()
    if (!code || joining.value) return false
    joining.value = true
    try {
      const res = await redeemInvitation(code)
      const role = ROLE_LABELS[res.role] ?? res.role
      toast.success(`已加入工作区「${res.workspace.name}」（${role}）`)
      void workspaces.load(true)
      await router.push(`/workspaces/${res.workspace.id}`)
      return true
    } catch (e) {
      if (isApiErrorCode(e, 'ALREADY_MEMBER')) {
        void workspaces.load(true)
        await router.push('/')
        return true
      }
      return false // 其余错误：全局 toast 已呈现，留在原地重试
    } finally {
      joining.value = false
    }
  }

  return { joining, join }
}
