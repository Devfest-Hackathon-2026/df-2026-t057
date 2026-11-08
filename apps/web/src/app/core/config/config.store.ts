import { Injectable, signal } from '@angular/core';
import { createApiClient, type Config } from '@dopagaki/contract';
import { Operation } from '../http/operation';
import { requireData } from '../http/api-client';

/** `GET /api/config`。認証の前に読むので、認証なしのクライアントを使う。 */
@Injectable({ providedIn: 'root' })
export class ConfigStore {
  private readonly client = createApiClient({
    origin: location.origin,
    authorization: () => undefined,
  });
  readonly config = signal<Config | undefined>(undefined);
  readonly load = new Operation();

  fetch(): Promise<boolean> {
    return this.load
      .run(
        () => requireData(this.client.GET('/config')),
        (config) => {
          this.config.set(config);
        },
      )
      .then((outcome) => outcome.ok);
  }
}
