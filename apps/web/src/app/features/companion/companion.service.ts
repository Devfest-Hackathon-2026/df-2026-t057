import { Injectable, inject } from '@angular/core';
import type { CompanionMoment } from '@dopagaki/contract';
import { companionFallbacks } from '@dopagaki/messages';
import { API_CLIENT, idempotent, requireData } from '../../core/http/api-client';

/** 相棒の一言。取得できなければ決まった文を使う（表示を止めない）。 */
@Injectable({ providedIn: 'root' })
export class CompanionService {
  private readonly api = inject(API_CLIENT);

  async line(moment: CompanionMoment, taskId?: string): Promise<string> {
    try {
      const result = await requireData(
        this.api.POST('/companion/lines', {
          params: { header: idempotent() },
          body: taskId === undefined ? { moment } : { moment, taskId },
        }),
      );
      return result.line;
    } catch {
      return companionFallbacks[moment];
    }
  }
}
