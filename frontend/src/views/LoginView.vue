<template>
  <div class="login-page">
    <svg class="login-bg" viewBox="0 0 1200 800" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
      <circle cx="180" cy="160" r="150" fill="#ffd6c2" opacity=".55" />
      <circle cx="1040" cy="640" r="190" fill="#e6ddff" opacity=".6" />
      <circle cx="980" cy="140" r="90" fill="#fff0b8" opacity=".7" />
      <circle cx="260" cy="680" r="70" fill="#cdeee0" opacity=".7" />
      <path d="M560 70 l10 24 l24 10 l-24 10 l-10 24 l-10 -24 l-24 -10 l24 -10 z" fill="#f5c451" opacity=".8" />
      <path d="M860 420 l7 16 l16 7 l-16 7 l-7 16 l-7 -16 l-16 -7 l16 -7 z" fill="#f2a38c" opacity=".8" />
    </svg>
    <div class="login-card">
      <div class="login-mascot"><Mascot mood="wave" :size="120" float /></div>
      <h2>欢迎回来</h2>
      <p>小Q 已经准备好啦，登录后一起照看客服吧</p>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item label="账号" prop="username"><el-input v-model="form.username" size="large" autocomplete="username" placeholder="管理员账号" :prefix-icon="User" /></el-form-item>
        <el-form-item label="密码" prop="password"><el-input v-model="form.password" size="large" autocomplete="current-password" placeholder="密码" type="password" show-password :prefix-icon="Lock" @keyup.enter="submit" /></el-form-item>
        <div class="login-options"><el-checkbox v-model="remember">保持登录</el-checkbox><span v-if="mockFallbackEnabled">开发预览：admin / admin123456</span></div>
        <el-button type="primary" size="large" native-type="submit" :loading="auth.loading" class="login-submit">登录</el-button>
      </el-form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Lock, User } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'
import Mascot from '@/components/fx/Mascot.vue'
import { useAuthStore } from '@/stores/auth'
import { mockFallbackEnabled } from '@/api/client'

const router = useRouter(); const route = useRoute(); const auth = useAuthStore(); const formRef = ref<FormInstance>(); const remember = ref(true); const error = ref('')
const form = reactive({ username: mockFallbackEnabled ? 'admin' : '', password: mockFallbackEnabled ? 'admin123456' : '' })
const rules: FormRules = { username: [{ required: true, message: '请输入管理员账号', trigger: 'blur' }], password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }] }
async function submit() { if (!(await formRef.value?.validate().catch(() => false))) return; error.value = ''; try { await auth.login(form.username, form.password); router.replace(String(route.query.redirect || '/overview')) } catch (e) { error.value = e instanceof Error ? e.message : '登录失败，请检查账号密码' } }
</script>
