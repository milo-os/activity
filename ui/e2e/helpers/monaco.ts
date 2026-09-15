import type { Page } from '@playwright/test';

/**
 * Wait until the CEL editor container has finished loading: either Monaco
 * has mounted (`.monaco-editor`) or a plain textarea is rendered.
 *
 * Monaco 0.50+ uses the EditContext API in Chromium, so its input is a
 * `div[role="textbox"]` rather than the `textarea.inputarea` older builds
 * rendered. Detect the editor by its root element, not by the input.
 */
async function waitForEditor(page: Page, testId: string) {
  const editor = page.getByTestId(testId);
  await editor.waitFor({ state: 'visible', timeout: 10000 });

  // Wait up to 8 seconds for Monaco or the fallback (Monaco can be slow in CI)
  await page.waitForFunction(
    (testId) => {
      const container = document.querySelector(`[data-testid="${testId}"]`);
      if (!container) return false;
      if (container.textContent?.includes('Loading editor...')) return false;
      if (container.querySelector('.monaco-editor .view-lines')) return true;
      return container.querySelector('textarea') !== null;
    },
    testId,
    { timeout: 8000 }
  );

  return editor;
}

/**
 * Find the Monaco editor instance mounted inside the container and run `fn`
 * against it in the page. Returns `undefined` when Monaco is not mounted or
 * the global `monaco` namespace is unavailable.
 */
async function withMonacoInstance<T, A = undefined>(
  page: Page,
  testId: string,
  fn: (editor: { getValue(): string; setValue(v: string): void }, arg: A) => T,
  arg?: A
): Promise<T | undefined> {
  return page.evaluate(
    ({ testId, fnSource, arg }) => {
      type Instance = { getDomNode(): HTMLElement | null; getValue(): string; setValue(v: string): void };
      const container = document.querySelector(`[data-testid="${testId}"]`);
      const monaco = (window as unknown as { monaco?: { editor?: { getEditors?: () => Instance[] } } }).monaco;
      if (!container || !monaco?.editor?.getEditors) return undefined;
      const instance = monaco.editor.getEditors().find((e) => {
        const node = e.getDomNode();
        return node !== null && container.contains(node);
      });
      if (!instance) return undefined;
      return new Function('editor', 'arg', `return (${fnSource})(editor, arg);`)(instance, arg) as T;
    },
    { testId, fnSource: fn.toString(), arg }
  );
}

/**
 * Type text into a Monaco editor OR fallback textarea.
 * Monaco editors don't support standard page.fill() because they use a
 * complex DOM structure. Prefer setting the model value through Monaco's own
 * API (which fires onChange like typing does); fall back to keyboard input.
 */
export async function fillMonacoEditor(page: Page, testId: string, text: string) {
  const editor = await waitForEditor(page, testId);
  const hasMonaco = (await editor.locator('.monaco-editor').count()) > 0;

  if (!hasMonaco) {
    await editor.locator('textarea').first().fill(text);
    return;
  }

  const setViaApi = await withMonacoInstance(
    page,
    testId,
    (instance, value: string) => {
      instance.setValue(value);
      return true;
    },
    text
  ).catch(() => undefined);

  if (setViaApi) return;

  // Keyboard fallback: focus the editor, select all, replace
  await editor.locator('.monaco-editor').click();
  await page.waitForTimeout(100);
  await page.keyboard.press('ControlOrMeta+A');
  await page.keyboard.type(text, { delay: 5 });
}

/**
 * Get the value from a Monaco editor OR fallback textarea
 */
export async function getMonacoEditorValue(page: Page, testId: string): Promise<string> {
  let editor;
  try {
    editor = await waitForEditor(page, testId);
  } catch {
    const content = await page.getByTestId(testId).textContent();
    throw new Error(`Editor not ready for test-id ${testId}. Content: ${content}`);
  }

  const hasMonaco = (await editor.locator('.monaco-editor').count()) > 0;
  if (!hasMonaco) {
    return editor.locator('textarea').first().inputValue();
  }

  const viaApi = await withMonacoInstance(page, testId, (instance) => instance.getValue()).catch(
    () => undefined
  );
  if (typeof viaApi === 'string') return viaApi;

  // Monaco renders content in .view-line elements (with non-breaking spaces)
  const lines = await editor.locator('.view-line').allTextContents();
  return lines.join('\n').replace(/\u00a0/g, ' ').trim();
}

/**
 * Check if a Monaco editor has a specific value
 * Useful for assertions in tests
 */
export async function expectMonacoEditorValue(page: Page, testId: string, expectedValue: string) {
  const actualValue = await getMonacoEditorValue(page, testId);
  return actualValue === expectedValue;
}
