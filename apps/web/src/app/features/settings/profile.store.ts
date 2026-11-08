import { Injectable } from '@angular/core';
import type { Profile, ProfileInput, Resource } from '@dopagaki/contract';
import { idempotent, ifMatch, requireData } from '../../core/http/api-client';
import { Operation, type Outcome } from '../../core/http/operation';
import { NotLoadedError } from '../../core/store/not-loaded';
import { ResourceStore } from '../../core/store/resource-store';

/** 本人の情報（表示名・作業の傾向・タイムゾーン）。 */
@Injectable({ providedIn: 'root' })
export class ProfileStore extends ResourceStore<Profile> {
  protected readonly resources: readonly Resource[] = ['profile'];
  readonly save = new Operation(this.scope);
  readonly remember = new Operation(this.scope);

  protected fetch(): Promise<Profile> {
    return requireData(this.api.GET('/profile'));
  }

  protected override revisionOf(value: Profile): number {
    return value.revision;
  }

  update(input: ProfileInput): Promise<Outcome<Profile>> {
    return this.save.run(
      () => {
        const current = this.value();
        if (!current) throw new NotLoadedError('profile');
        return requireData(
          this.api.PUT('/profile', {
            params: { header: ifMatch(current.revision) },
            body: input,
          }),
        );
      },
      (profile) => {
        this.accept(profile);
      },
    );
  }

  /** 相談のカードから、作業の傾向に1行追加する。 */
  rememberLine(text: string): Promise<Outcome<Profile>> {
    return this.remember.run(
      () =>
        requireData(
          this.api.POST('/assistant/memories', {
            params: { header: idempotent() },
            body: { text },
          }),
        ),
      (profile) => {
        this.accept(profile);
      },
    );
  }
}
