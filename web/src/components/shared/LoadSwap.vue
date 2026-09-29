<!-- 骨架 ⇄ 内容 的平滑交换容器（消除加载完成瞬间的布局跳动）：
     - 问题：骨架屏与真实内容高度不一致时，v-if 硬切换会产生
       矮→高→矮 的跳变（数据少时尤其明显）；
     - 手法：两层各占一个 grid 行，行高在 0fr ↔ 1fr 间过渡（CSS 原生的
       height:auto 动画等价物），配 opacity 交叉淡换，200ms ease-out 收尾；
       容器高度取两层行高的和，切换全程无回退到 0 的瞬间；
     - 折叠层置 inert：隐藏内容不可聚焦/不可交互，不抢焦点；
     - 两层常驻挂载：默认插槽在 loading 期间也会渲染（空数组 v-for 零成本），
       重内容（编辑器等懒 chunk）由调用方惰性化后再放入；
     - 容器高度语义：根高 auto 时（常规流内），行高随各自内容走；根有确定
       高度时（flex-1/h-full 传入），1fr 行填满根、内层滚动区用 h-full 承接。
     - motion-reduce 下不做动画，直接硬切（尊重系统减少动效偏好）。 -->
<script setup lang="ts">
defineProps<{
  /** true = 骨架层展开（内容层折叠）；false = 反之 */
  loading: boolean
}>()
</script>

<template>
  <div>
    <div
      class="grid transition-all duration-200 ease-out motion-reduce:transition-none"
      :style="{ gridTemplateRows: loading ? '1fr' : '0fr', opacity: loading ? 1 : 0 }"
      :inert="!loading"
    >
      <div class="min-h-0 overflow-hidden">
        <slot name="skeleton" />
      </div>
    </div>
    <div
      class="grid transition-all duration-200 ease-out motion-reduce:transition-none"
      :style="{ gridTemplateRows: loading ? '0fr' : '1fr', opacity: loading ? 0 : 1 }"
      :inert="loading"
    >
      <div class="min-h-0 overflow-hidden">
        <slot />
      </div>
    </div>
  </div>
</template>
