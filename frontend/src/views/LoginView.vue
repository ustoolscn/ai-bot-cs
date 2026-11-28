<template>
  <div class="login-universe">
    <NeuralCoreCanvas mode="login" class="login-webgl" />
    <div class="login-vignette" aria-hidden="true" />

    <div class="login-stage">
      <section class="hero-copy">
        <div class="hero-badge"><i class="pulse-dot" /><span>WEBGL NEURAL COMMAND</span></div>
        <h1>AI Bot Hub</h1>
        <p class="hero-lead">面向 QQ 群聊与私聊的下一代智能客服与三维认知知识中枢。</p>
        <div class="hero-chips">
          <span>3D 向量知识宇宙</span>
          <span>3D 跨群认知记忆星图</span>
          <span>实时情绪预判导演层</span>
        </div>
      </section>

      <div class="login-card tech-cockpit">
        <div class="card-glow-line" aria-hidden="true" />
        <div class="login-brand">
          <div class="brand-halo"><Mascot head-only :size="38" /></div>
          <div>
            <strong>控制台登录</strong>
            <span>进入 AI 中枢与 3D 认知宇宙</span>
          </div>
        </div>
        <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
          <el-form-item label="管理员账号" prop="username"><el-input v-model="form.username" size="large" autocomplete="username" placeholder="管理员账号" /></el-form-item>
          <el-form-item label="访问密码" prop="password" :error="error"><el-input v-model="form.password" size="large" autocomplete="current-password" placeholder="密码" type="password" show-password @keyup.enter="submit" /></el-form-item>
          <div class="login-options"><el-checkbox v-model="remember">保持登录</el-checkbox><span v-if="mockFallbackEnabled">开发预览账号已自动填好</span></div>
          <el-button type="primary" size="large" native-type="submit" :loading="auth.loading" class="login-submit">接入控制台</el-button>
        </el-form>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { FormInstance, FormRules } from 'element-plus'
import Mascot from '@/components/fx/Mascot.vue'
import NeuralCoreCanvas from '@/components/three/NeuralCoreCanvas.vue'
import { useAuthStore } from '@/stores/auth'
import { mockFallbackEnabled } from '@/api/client'
import { useSpotlight } from '@/utils/spotlight'

useSpotlight()

const router = useRouter(); const route = useRoute(); const auth = useAuthStore(); const formRef = ref<FormInstance>(); const remember = ref(true); const error = ref('')
const form = reactive({ username: mockFallbackEnabled ? 'admin' : '', password: mockFallbackEnabled ? 'admin123456' : '' })
const rules: FormRules = { username: [{ required: true, message: '请输入管理员账号', trigger: 'blur' }], password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }] }
async function submit() { if (!(await formRef.value?.validate().catch(() => false))) return; error.value = ''; try { await auth.login(form.username, form.password); router.replace(String(route.query.redirect || '/overview')) } catch (e) { error.value = e instanceof Error ? e.message : '登录失败，请检查账号密码' } }
</script>

<style scoped>
.login-universe {
  position: relative;
  display: grid;
  place-items: center;
  min-height: 100vh;
  min-height: 100dvh;
  padding: 28px;
  overflow: hidden;
  background: radial-gradient(circle at 28% 45%, #0b1830 0%, #050914 68%, #02040a 100%);
}
.login-webgl { position: absolute; inset: 0; z-index: 1; }
.login-vignette {
  position: absolute;
  inset: 0;
  z-index: 2;
  background: radial-gradient(circle at 30% 50%, transparent 20%, rgba(2, 5, 14, 0.55) 85%);
  pointer-events: none;
}

.login-stage {
  position: relative;
  z-index: 5;
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) 400px;
  align-items: center;
  gap: 48px;
  width: 100%;
  max-width: 1120px;
}

.hero-copy { display: flex; flex-direction: column; gap: 16px; color: #f8fafc; pointer-events: none; }
.hero-badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  align-self: flex-start;
  padding: 5px 12px;
  border: 1px solid rgba(56, 189, 248, 0.36);
  border-radius: 999px;
  background: rgba(14, 165, 233, 0.14);
  color: #7dd3fc;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.1em;
  backdrop-filter: blur(10px);
}
.pulse-dot { width: 7px; height: 7px; border-radius: 50%; background: #38bdf8; box-shadow: 0 0 10px #38bdf8; animation: pulse 2s ease-in-out infinite; }
.hero-copy h1 {
  margin: 0;
  font-size: clamp(36px, 5vw, 56px);
  font-weight: 800;
  letter-spacing: -0.02em;
  line-height: 1.08;
  background: linear-gradient(125deg, #ffffff 15%, #7dd3fc 58%, #a5b4fc 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}
.hero-lead { max-width: 36ch; margin: 0; color: #cbd5e1; font-size: 16px; line-height: 1.7; }
.hero-chips { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 4px; }
.hero-chips span {
  padding: 6px 12px;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.62);
  color: #e2e8f0;
  font-size: 12px;
  backdrop-filter: blur(10px);
}

.tech-cockpit {
  width: 100%;
  padding: 32px 30px 28px;
  border-radius: 20px;
  border: 1px solid rgba(148, 163, 184, 0.22);
  background: rgba(10, 17, 32, 0.78);
  color: #eef2ff;
  box-shadow: 0 28px 80px rgba(0, 0, 0, 0.68), inset 0 1px 0 rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(22px);
  overflow: hidden;
  animation: pop-in var(--dur-slow) var(--ease);
}
.tech-cockpit :deep(.el-form-item__label) { color: #cbd5e1; }
.tech-cockpit :deep(.el-input__wrapper) {
  background-color: rgba(6, 11, 22, 0.78);
  box-shadow: 0 0 0 1px rgba(148, 163, 184, 0.24) inset;
}
.tech-cockpit :deep(.el-input__inner) { color: #f8fafc; }
.tech-cockpit .login-options { color: #94a3b8; }
.tech-cockpit .login-brand strong { color: #f8fafc; }
.tech-cockpit .login-brand span { color: #94a3b8; }

.card-glow-line { position: absolute; top: 0; left: 10%; right: 10%; height: 2px; background: linear-gradient(90deg, transparent, #38bdf8, #818cf8, transparent); opacity: 0.9; }
.brand-halo { display: grid; place-items: center; width: 48px; height: 48px; border-radius: 14px; background: rgba(56, 189, 248, 0.14); box-shadow: 0 0 24px rgba(56, 189, 248, 0.28); }

@keyframes pulse { 0%, 100% { opacity: 1; transform: scale(1); } 50% { opacity: 0.45; transform: scale(0.8); } }

@media (max-width: 900px) {
  .login-stage { grid-template-columns: 1fr; max-width: 420px; gap: 24px; }
  .hero-copy { align-items: center; text-align: center; }
  .hero-badge { align-self: center; }
  .hero-chips { justify-content: center; }
}
</style>
