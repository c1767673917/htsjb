// Vue Router configuration. 7 routes total:
//   /          — defaults to /y2021
//   /y2021 .. /y2025 — per-year collection pages (public)
//   /admin/login — password prompt
//   /admin       — back office (guarded by `beforeEnter` → ping)
//
// The admin guard calls GET /api/admin/ping and redirects to /admin/login on
// 401 (FR-ADMIN-AUTH, architecture §2 frontend row).

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router';
import CollectionView from '@/views/CollectionView.vue';
import AdminLoginView from '@/views/AdminLoginView.vue';
import AdminView from '@/views/AdminView.vue';
import { useAdminStore } from '@/stores/admin';
import { setOn401Handler } from '@/lib/api';

export const YEARS = [2021, 2022, 2023, 2024, 2025] as const;
export type CollectionYear = (typeof YEARS)[number];

const collectionRoutes: RouteRecordRaw[] = YEARS.map((year) => ({
  path: `/y${year}`,
  name: `y${year}`,
  component: CollectionView,
  props: { year },
  meta: { title: `${year} 年度收集` },
}));

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/y2021' },
  ...collectionRoutes,
  {
    path: '/admin/login',
    name: 'admin-login',
    component: AdminLoginView,
    meta: { title: '管理员登录' },
  },
  {
    path: '/admin',
    name: 'admin',
    component: AdminView,
    meta: { title: '后台管理', requiresAdmin: true },
    beforeEnter: async (_to, _from, next) => {
      const store = useAdminStore();
      const ok = await store.ping();
      if (ok) return next();
      return next({ path: '/admin/login', query: { redirect: '/admin' } });
    },
  },
  // Fallback: unknown paths redirect home so the Go SPA history fallback
  // never ends on a blank screen.
  { path: '/:pathMatch(.*)*', redirect: '/y2021' },
];

export const router = createRouter({
  history: createWebHistory(),
  routes,
});

// Any /api/admin/* call that comes back as 401 — including those that happen
// after route-transition, e.g. session expired mid-session — should redirect
// to /admin/login. Registered here so api.ts stays router-agnostic.
setOn401Handler(() => {
  if (router.currentRoute.value.path.startsWith('/admin')) {
    router.replace({ path: '/admin/login', query: { redirect: router.currentRoute.value.fullPath } });
  }
});

router.afterEach((to) => {
  const title = to.meta?.title as string | undefined;
  if (title) document.title = `${title} · 单据收集`;
});
