// Mock 数据模式开关（dev-only 设计演示）：VITE_TASKS_MOCK=1 时任务视图与
// 事件流走内存实现（mocks/taskMock），完全不依赖后端；置 0 或删除该 env 恢复真实 API。
// session store 也读它做演示会话兜底，因此放 lib（避免 store↔api 循环依赖）。
export const TASKS_MOCK: boolean =
  import.meta.env.DEV && import.meta.env.VITE_TASKS_MOCK === '1'
