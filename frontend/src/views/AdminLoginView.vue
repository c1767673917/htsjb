<script setup lang="ts">
// Password-only login prompt for /admin. On success the admin store's ping()
// populates the CSRF token, then we navigate to the redirect path (default
// /admin). Implements FR-ADMIN-AUTH.
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAdminStore } from '@/stores/admin';

const router = useRouter();
const route = useRoute();
const admin = useAdminStore();

const password = ref('');
const submitting = ref(false);

async function submit(e: Event) {
  e.preventDefault();
  if (!password.value) return;
  submitting.value = true;
  try {
    const ok = await admin.login(password.value);
    if (ok) {
      const redirect = (route.query.redirect as string) || '/admin';
      await router.replace(redirect);
    }
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="login-box">
    <h1 style="margin: 0; font-size: var(--font-xl)">管理员登录</h1>
    <p class="muted" style="margin: 0; font-size: var(--font-sm)">
      请输入后台管理员密码（由 <code>config.yaml</code> 配置）。
    </p>
    <form @submit="submit">
      <label for="admin-password" class="sr-only">密码</label>
      <input
        id="admin-password"
        v-model="password"
        class="input"
        type="password"
        autocomplete="current-password"
        placeholder="请输入密码"
        required
        :disabled="submitting"
      />
      <div style="margin-top: var(--space-3); display: flex; gap: var(--space-3)">
        <button type="submit" class="btn btn-primary tap" :disabled="submitting || !password">
          <span v-if="submitting" class="spinner" style="margin-right: 8px" />
          {{ submitting ? '登录中…' : '登录' }}
        </button>
        <RouterLink to="/y2021" class="btn btn-ghost tap" style="text-decoration:none">
          返回收集页
        </RouterLink>
      </div>
    </form>
  </div>
</template>
