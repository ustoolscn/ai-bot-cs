<template>
  <!-- 同样的提示合并成一条并计数，最多同时显示 3 条 -->
  <el-config-provider :message="{ max: 3, grouping: true }"><router-view /></el-config-provider>
</template>
