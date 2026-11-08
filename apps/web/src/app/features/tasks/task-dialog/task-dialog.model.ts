/** TaskDialog に渡す値。`taskId` があれば編集、なければ登録。 */
export interface TaskDialogData {
  readonly taskId?: string;
  /** 開いたときのモード。登録は手入力、編集は AI に相談（旧実装と同じ）。 */
  readonly mode?: 'manual' | 'ai';
}
