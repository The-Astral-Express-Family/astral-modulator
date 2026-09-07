<script setup lang="ts">
// Device Flow 人类审批页：CLI 发起登录后，verification_uri 指向 /device?code=ABCD-EFGH。
// 本页是 CLI 登录闭环的服务端配套，Phase 1 必须实装（roadmap Phase 1）。
//
// 流程（architecture §8.2）：
//   1. 用户打开 CLI 给出的 URL（或手输 user_code）；
//   2. 浏览器完成人类登录（本页 redirect 到登录，TODO(phase-1)）；
//   3. 展示 device 授权请求详情，用户 Approve / Deny；
//   4. 服务端把 device_authorization 标记 approved/denied，CLI 轮询后拿到 token。
//
// TODO(phase-1): 需要的 API（尚未在 openapi 定稿，定稿时同步登记）：
//   GET  /api/v1/auth/device/authorizations?user_code=...  （取 pending 请求详情）
//   POST /api/v1/auth/device/authorizations/{id}/approve
//   POST /api/v1/auth/device/authorizations/{id}/deny
import { computed } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()
const userCode = computed(() => (typeof route.query.code === 'string' ? route.query.code : ''))
</script>

<template>
  <h2>设备授权</h2>
  <div class="card">
    <p>user_code: <code>{{ userCode || '（未提供，请输入 CLI 显示的代码）' }}</code></p>
    <p class="muted">
      确认这个代码与 CLI 终端显示一致后批准。批准后 CLI 将获得访问凭证。
    </p>
    <!-- TODO(phase-1): 登录门槛 + 授权请求详情（client type、发起时间）+ Approve/Deny 按钮。 -->
  </div>
</template>
