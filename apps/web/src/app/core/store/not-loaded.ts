/** 読み込む前に、版が必要な更新をしようとした。 */
export class NotLoadedError extends Error {
  constructor(resource: string) {
    super(`${resource} is not loaded`);
    this.name = 'NotLoadedError';
  }
}
