<script setup lang="ts">
// Root shell. Renders the active route and mounts the global Toast stack.
import Toast from '@/components/Toast.vue';
</script>

<template>
  <div class="app-shell">
    <RouterView />
    <Toast />
  </div>
</template>
