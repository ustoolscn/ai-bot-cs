<template>
  <div class="login-page">
    <section class="login-intro">
      <BrandLogo />
      <div><span class="eyebrow">QQ AI 机器人管理平台</span><h1>把每一次群聊，<br />变成可靠的智能服务。</h1><p>统一管理机器人、模型、知识库与消息处理链路，让 AI 回复清晰、可控、可追踪。</p></div>
      <div class="login-feature-list"><span><el-icon><CircleCheckFilled /></el-icon>QQ Webhook 统一接入</span><span><el-icon><CircleCheckFilled /></el-icon>PostgreSQL + pgvector 知识检索</span><span><el-icon><CircleCheckFilled /></el-icon>完整消息链路观测</span></div>
    </section>
    <section class="login-form-panel">
      <div class="login-card">
        <div class="mobile-login-brand"><BrandLogo /></div>
        <h2>欢迎回来</h2><p>登录 AI Bot Hub 管理控制台</p>
        <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
        <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
          <el-form-item label="管理员账号" prop="username"><el-input v-model="form.username" size="large" autocomplete="username" placeholder="请输入管理员账号" :prefix-icon="User" /></el-form-item>
          <el-form-item label="密码" prop="password"><el-input v-model="form.password" size="large" autocomplete="current-password" placeholder="请输入密码" type="password" show-password :prefix-icon="Lock" @keyup.enter="submit" /></el-form-item>
          <div class="login-options"><el-checkbox v-model="remember">保持登录</el-checkbox><span v-if="mockFallbackEnabled">开发预览：admin / admin123456</span></div>
          <el-button type="primary" size="large" native-type="submit" :loading="auth.loading" class="login-submit">登录管理端</el-button>
        </el-form>
        <p class="login-foot">登录即表示你同意遵守系统的安全与隐私规范</p>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { CircleCheckFilled, Lock, User } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import BrandLogo from '@/components/BrandLogo.vue'
import { useAuthStore } from '@/stores/auth'
import { mockFallbackEnabled } from '@/api/client'

const router = useRouter(); const route = useRoute(); const auth = useAuthStore(); const formRef = ref<FormInstance>(); const remember = ref(true); const error = ref('')
const form = reactive({ username: mockFallbackEnabled ? 'admin' : '', password: mockFallbackEnabled ? 'admin123456' : '' })
const rules: FormRules = { username: [{ required: true, message: '请输入管理员账号', trigger: 'blur' }], password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }] }
async function submit() { if (!(await formRef.value?.validate().catch(() => false))) return; error.value = ''; try { await auth.login(form.username, form.password); router.replace(String(route.query.redirect || '/overview')) } catch (e) { error.value = e instanceof Error ? e.message : '登录失败，请检查账号密码' } }
</script>
