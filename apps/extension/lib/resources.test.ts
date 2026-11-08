import { describe, expect, it } from 'vitest';
import { resourcesToRefetch } from './resources';

describe('resourcesToRefetch', () => {
  it('refetches only the named resource when it is newer', () => {
    expect(resourcesToRefetch({ resource: 'tasks', revision: 4 }, { tasks: 3 })).toEqual(['tasks']);
    expect(resourcesToRefetch({ resource: 'tasks', revision: 3 }, { tasks: 3 })).toEqual([]);
    expect(resourcesToRefetch({ resource: 'tasks', revision: 2 }, { tasks: 3 })).toEqual([]);
  });

  it('refetches a resource it has not seen yet', () => {
    expect(resourcesToRefetch({ resource: 'sleep', revision: 0 }, {})).toEqual(['sleep']);
  });

  it('refreshes the viewing allowance with the wallet', () => {
    expect(resourcesToRefetch({ resource: 'wallet', revision: 9 }, {})).toEqual([
      'wallet',
      'viewing',
    ]);
  });

  it('refetches the profile to follow its language', () => {
    expect(resourcesToRefetch({ resource: 'profile', revision: 2 }, { profile: 1 })).toEqual([
      'profile',
    ]);
  });

  it('ignores resources the extension does not show', () => {
    expect(resourcesToRefetch({ resource: 'assistant', revision: 9 }, {})).toEqual([]);
    expect(resourcesToRefetch({ resource: 'calendar', revision: 9 }, {})).toEqual([]);
  });
});
