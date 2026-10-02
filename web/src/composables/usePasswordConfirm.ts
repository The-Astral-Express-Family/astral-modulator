// 二次密码确认（注册/重置密码共用；2026-10-01 生产 case：注册口令输错 →
// 无法登录且无自助恢复，故两处入口都要求确认）。confirmMismatch 供视图渲染
// 行内提示与 aria-invalid、禁用提交按钮；ensureMatch() 是 submit 前的兜底
// 闸（不一致弹 toast 并返回 false——防绕过禁用按钮的隐式提交路径）。
import { computed, ref } from 'vue'
import { toast } from 'vue-sonner'

export function usePasswordConfirm() {
  const password = ref('')
  const passwordConfirm = ref('')
  const confirmMismatch = computed(
    () => passwordConfirm.value !== '' && passwordConfirm.value !== password.value,
  )
  function ensureMatch(): boolean {
    if (password.value !== passwordConfirm.value) {
      toast.error('两次输入的密码不一致')
      return false
    }
    return true
  }
  return { password, passwordConfirm, confirmMismatch, ensureMatch }
}
