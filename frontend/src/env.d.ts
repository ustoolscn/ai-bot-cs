/// <reference types="vite/client" />

declare module 'vue' {
  interface ComponentCustomProperties {
    /** 把 <@openid> 换成 @昵称 */
    $at: typeof import('./utils/mentions').formatMentions
  }
}
export {}
