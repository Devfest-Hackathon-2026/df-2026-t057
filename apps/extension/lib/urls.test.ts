import { describe, expect, it } from 'vitest';
import { appUrl, safeHttpUrl, workSitePattern, workSiteScriptId } from './urls';

describe('safeHttpUrl', () => {
  it.each([
    ['https://example.com/doc', 'https://example.com/doc'],
    ['http://localhost:3000/', 'http://localhost:3000/'],
    ['javascript:alert(1)', undefined],
    ['', undefined],
    ['chrome://extensions', undefined],
  ])('%s → %s', (value, expected) => {
    expect(safeHttpUrl(value)).toBe(expected);
  });
});

describe('work site', () => {
  it('asks for the origin of the current page', () => {
    expect(workSitePattern('https://docs.example.com/a/b?c')).toBe('https://docs.example.com/*');
    expect(workSitePattern('chrome://newtab')).toBeUndefined();
    expect(workSitePattern(undefined)).toBeUndefined();
  });
  it('derives a stable script id', () => {
    expect(workSiteScriptId('https://docs.example.com/*')).toBe('work-https___docs_example_com__');
  });
});

describe('appUrl', () => {
  it('builds management URLs with a query', () => {
    expect(appUrl('https://app.example')).toBe('https://app.example/');
    expect(appUrl('https://app.example', { notification: 'task-1' })).toBe(
      'https://app.example/?notification=task-1',
    );
  });
});
