<template>
  <aside class="filter-sidebar" :class="{ open: modelValue }">
    <div class="filter-title"><el-icon><Operation /></el-icon><strong>运行概览</strong><el-button class="filter-close" text circle @click="$emit('update:modelValue', false)"><el-icon><Close /></el-icon></el-button></div>
    <el-form label-position="top">
      <el-form-item label="机器人"><el-select v-model="local.bot" placeholder="全部机器人"><el-option label="全部机器人" value="all" /></el-select></el-form-item>
      <el-form-item label="时间范围"><el-date-picker v-model="local.range" type="datetimerange" start-placeholder="开始时间" end-placeholder="结束时间" format="MM-DD HH:mm" value-format="YYYY-MM-DD HH:mm:ss" /></el-form-item>
      <el-form-item label="对比"><el-select v-model="local.compare"><el-option label="与昨日对比" value="day" /><el-option label="与上周对比" value="week" /><el-option label="不对比" value="none" /></el-select></el-form-item>
      <el-form-item label="QQ 端类型"><el-select v-model="local.client"><el-option label="全部" value="all" /><el-option label="群聊" value="group" /><el-option label="单聊" value="c2c" /></el-select></el-form-item>
      <el-form-item label="消息类型"><el-select v-model="local.message"><el-option label="全部" value="all" /><el-option label="@消息" value="mention" /><el-option label="全量消息" value="normal" /></el-select></el-form-item>
      <el-form-item label="会话类型"><el-select v-model="local.conversation"><el-option label="全部" value="all" /><el-option label="群聊" value="group" /><el-option label="单聊" value="c2c" /></el-select></el-form-item>
    </el-form>
    <div class="filter-buttons"><el-button type="primary" @click="apply">应用筛选</el-button><el-button @click="reset">重置</el-button></div>
    <p class="filter-updated">数据按页面刷新 <el-icon><RefreshRight /></el-icon></p>
  </aside>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import { Close, Operation, RefreshRight } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const defaults = { bot: 'all', range: [] as string[], compare: 'day', client: 'all', message: 'all', conversation: 'all' }
const local = reactive({ ...defaults })
function apply() { emit('update:modelValue', false); ElMessage.success('筛选条件已应用') }
function reset() { Object.assign(local, defaults); ElMessage.info('筛选条件已重置') }
</script>
