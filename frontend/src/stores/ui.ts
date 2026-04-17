// Global UI store: toast queue + global loading counter. Kept minimal so any
// component can enqueue feedback without routing through props.

import { defineStore } from 'pinia';
import { ref } from 'vue';

export type ToastKind = 'success' | 'error' | 'info';

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
  duration: number;
}

const DEFAULT_DURATION = 2500; // NFR-UX-5: 2.5s auto-dismiss.

let nextId = 1;

export const useUiStore = defineStore('ui', () => {
  const toasts = ref<Toast[]>([]);
  const loadingCount = ref(0);

  function pushToast(kind: ToastKind, message: string, duration = DEFAULT_DURATION) {
    const id = nextId++;
    const toast: Toast = { id, kind, message, duration };
    toasts.value.push(toast);
    // Auto-dismiss in a separate tick so the caller does not have to await us.
    setTimeout(() => {
      toasts.value = toasts.value.filter((t) => t.id !== id);
    }, duration);
    return id;
  }

  function success(msg: string) {
    // Bonus UX: vibrate on successful submit when the hardware supports it.
    if (typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function') {
      try {
        navigator.vibrate(50);
      } catch {
        /* ignore */
      }
    }
    return pushToast('success', msg);
  }

  function error(msg: string) {
    return pushToast('error', msg);
  }

  function info(msg: string) {
    return pushToast('info', msg);
  }

  function dismiss(id: number) {
    toasts.value = toasts.value.filter((t) => t.id !== id);
  }

  function beginLoading() {
    loadingCount.value++;
  }
  function endLoading() {
    loadingCount.value = Math.max(0, loadingCount.value - 1);
  }

  return {
    toasts,
    loadingCount,
    pushToast,
    success,
    error,
    info,
    dismiss,
    beginLoading,
    endLoading,
  };
});
