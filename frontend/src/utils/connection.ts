import { reactive } from 'vue'

// 记录后端是否可达，供外壳顶部显示一条连接提示，代替各页面重复弹错。
export const connection = reactive({ lost: false, usingMock: false })
