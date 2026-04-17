import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import SearchBar from '@/components/SearchBar.vue';

/**
 * N-04: IME composition must not trigger the debounced search. Intermediate
 * Pinyin / 注音 keystrokes fire `input` events with `event.isComposing =
 * true` between `compositionstart` and `compositionend`; emitting `query`
 * during that window bounces the server on partial romanization.
 */
describe('SearchBar IME composition handling', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('N-04: skips the debounced query while a composition session is active', async () => {
    const wrapper = mount(SearchBar, {
      props: { modelValue: '', results: [], loading: false },
    });
    const input = wrapper.get('input');

    // Start composing — the user is mid-Pinyin.
    await input.trigger('compositionstart');

    // Simulate intermediate InputEvents during composition.
    (input.element as HTMLInputElement).value = 'ha';
    await input.trigger('input');
    (input.element as HTMLInputElement).value = 'han';
    await input.trigger('input');

    // Advance the debounce window; no query should have fired.
    vi.advanceTimersByTime(300);
    expect(wrapper.emitted('query')).toBeUndefined();

    // End the composition — now the final value should schedule a search.
    (input.element as HTMLInputElement).value = '哈尔滨';
    await input.trigger('compositionend');
    vi.advanceTimersByTime(300);

    const queries = wrapper.emitted('query');
    expect(queries).toBeDefined();
    expect(queries!.at(-1)).toEqual(['哈尔滨']);
  });

  it('N-04: plain (non-composition) input still fires after 250ms debounce', async () => {
    const wrapper = mount(SearchBar, {
      props: { modelValue: '', results: [], loading: false },
    });
    const input = wrapper.get('input');

    (input.element as HTMLInputElement).value = 'ab';
    await input.trigger('input');

    // Before the debounce window closes: nothing fired yet.
    vi.advanceTimersByTime(100);
    expect(wrapper.emitted('query')).toBeUndefined();

    vi.advanceTimersByTime(200);
    const queries = wrapper.emitted('query');
    expect(queries).toBeDefined();
    expect(queries!.at(-1)).toEqual(['ab']);
  });

  it('Mi-03: clearing the input cancels any pending debounce', async () => {
    const wrapper = mount(SearchBar, {
      props: { modelValue: 'hello', results: [], loading: false },
    });
    const input = wrapper.get('input');

    (input.element as HTMLInputElement).value = 'hello2';
    await input.trigger('input');
    // Click the clear button before the debounce fires.
    await wrapper.get('.clear-btn').trigger('click');

    vi.advanceTimersByTime(500);
    const queries = wrapper.emitted('query') ?? [];
    // The only `query` emission should be the explicit empty from onClear,
    // not the stale 'hello2' that the debounce would have sent.
    for (const call of queries) {
      expect(call[0]).not.toBe('hello2');
    }
    // And the clear itself must have fired with an empty string.
    expect(queries.at(-1)).toEqual(['']);
  });
});
