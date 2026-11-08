import { Injectable } from '@angular/core';
import type { Records, Resource } from '@dopagaki/contract';
import { requireData } from '../../core/http/api-client';
import { ResourceStore } from '../../core/store/resource-store';

/** 記録の画面の集計。集計はサーバーが行い、課題か残高が変わったら取り直す。 */
@Injectable({ providedIn: 'root' })
export class RecordsStore extends ResourceStore<Records> {
  protected readonly resources: readonly Resource[] = ['wallet', 'tasks'];

  protected fetch(): Promise<Records> {
    return requireData(this.api.GET('/records'));
  }
}
