<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-brand"><Mascot head-only :size="36" /><div><strong>AI Bot Hub</strong><span>QQ 客服管理后台</span></div></div>
      <h2>登录</h2>
      <p>使用管理员账号登录。</p>
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item label="账号" prop="username"><el-input v-model="form.username" size="large" autocomplete="username" placeholder="管理员账号" /></el-form-item>
        <el-form-item label="密码" prop="password" :error="error"><el-input v-model="form.password" size="large" autocomplete="current-password" placeholder="密码" type="password" show-password @keyup.enter="submit" /></el-form-item>
        <div class="login-options"><el-checkbox v-model="remember">保持登录</el-checkbox><span v-if="mockFallbackEnabled">开发预览账号已自动填好</span></div>
        <el-button type="primary" size="large" native-type="submit" :loading="auth.loading" class="login-submit">登录</el-button>
      </el-form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { FormInstance, FormRules } from 'element-plus'
import Mascot from '@/components/fx/Mascot.vue'
import { useAuthStore } from '@/stores/auth'
import { mockFallbackEnabled } from '@/api/client'

const router = useRouter(); const route = useRoute(); const auth = useAuthStore(); const formRef = ref<FormInstance>(); const remember = ref(true); const error = ref('')
const form = reactive({ username: mockFallbackEnabled ? 'admin' : '', password: mockFallbackEnabled ? 'admin123456' : '' })
const rules: FormRules = { username: [{ required: true, message: '请输入管理员账号', trigger: 'blur' }], password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }] }
async function submit() { if (!(await formRef.value?.validate().catch(() => false))) return; error.value = ''; try { await auth.login(form.username, form.password); router.replace(String(route.query.redirect || '/overview')) } catch (e) { error.value = e instanceof Error ? e.message : '登录失败，请检查账号密码' } }
</script>
