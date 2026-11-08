import { Injectable } from '@angular/core';
import type { Resource, Viewing } from '@dopagaki/contract';
import { requireData } from '../../core/http/api-client';
import { ResourceStore } from '../../core/store/resource-store';

/** 視聴の区切りの状態。区切りを始める・終えるのは拡張で行い、管理画面は表示だけ。 */
@Injectable({ providedIn: 'root' })
export class ViewingStore extends ResourceStore<Viewing> {
  protected readonly resources: readonly Resource[] = ['viewing'];

  protected fetch(): Promise<Viewing> {
    return requireData(this.api.GET('/viewing'));
  }
}
