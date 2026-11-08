import { TestBed } from '@angular/core/testing';
import { FakeBackend, provideFakeBackend } from '../../../testing/fake-backend';
import { settings, taskList, wallet, workSession } from '../../../testing/fixtures';
import { AssistantStore } from '../../features/assistant/assistant.store';
import { SettingsStore } from '../../features/settings/settings.store';
import { ShortcutStore } from '../../features/shortcut/shortcut.store';
import { TasksStore } from '../../features/tasks/tasks.store';
import { WalletStore } from '../../features/wallet/wallet.store';
import { WorkSessionStore } from '../../features/work/work-session.store';
import { AccountScope } from '../auth/account-scope';

describe('アカウントの切り替え', () => {
  it('すべての store と、手元にだけある状態を空に戻す', () => {
    TestBed.configureTestingModule({ providers: provideFakeBackend(new FakeBackend()) });
    const tasks = TestBed.inject(TasksStore);
    const settingsStore = TestBed.inject(SettingsStore);
    const walletStore = TestBed.inject(WalletStore);
    const work = TestBed.inject(WorkSessionStore);
    const shortcut = TestBed.inject(ShortcutStore);
    const assistant = TestBed.inject(AssistantStore);
    tasks.accept(taskList());
    settingsStore.accept(settings());
    walletStore.accept(wallet());
    work.accept(workSession());
    shortcut.issuedKey.set('secret');
    assistant.setDraft('workspace', '下書き');

    TestBed.inject(AccountScope).reset();

    expect(tasks.value()).toBeUndefined();
    expect(settingsStore.value()).toBeUndefined();
    expect(walletStore.value()).toBeUndefined();
    expect(work.value()).toBeUndefined();
    expect(shortcut.issuedKey()).toBeUndefined();
    expect(assistant.draft('workspace')).toBe('');
  });

  it('古い版の応答で新しい値を上書きしない', () => {
    TestBed.configureTestingModule({ providers: provideFakeBackend(new FakeBackend()) });
    const tasks = TestBed.inject(TasksStore);
    tasks.accept(taskList([], 5));
    tasks.accept(taskList([], 4));
    expect(tasks.revision()).toBe(5);
  });
});
