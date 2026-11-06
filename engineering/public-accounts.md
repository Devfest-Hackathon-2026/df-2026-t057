# 一般ユーザー対応

2026-11-06。対象はGoogleアカウントで誰でも利用し、それぞれ本人のカレンダーと、本人とDOPAGAKIの1対1のGoogle Chat DMを使う構成。

## アカウントの役割

| 役割 | 単位 | 扱うもの |
|---|---|---|
| 利用者 | 各自のGoogleアカウント | 自分の課題・残高・設定・専用カレンダー・個別DM |
| サービス実行 | Cloud Runのサービスアカウント | 同意済みの利用者ごとのトークンで処理。運営者の個人カレンダーやChatを代理利用しない |
| 公開・設定の担当 | Google Workspaceの運営担当者 | Chatアプリの設定と一般配布の申請。日常の実行にはログインし続けない |
| 運営権限 | Cloud IAMの複数管理者またはGoogleグループ | 一人の個人アカウントに依存せず管理できる。今回、グループ作成・IAM変更・組織移管は未実施 |

一般配布するChatアプリはDOPAGAKIとして共通で、会話と登録先を送信者ごとに分ける。利用者が個別にCloudプロジェクトやChatアプリを作る設計ではない。共有のスペースやグループ会話は受け付けない。

## 実装

- Firebaseの署名検証済みUIDをFirestoreの保存先に使う。リクエスト本文のユーザーIDやメールアドレスで保存先を決めない。
- カレンダー連携を始める時点で、署名検証済みFirebase ID tokenの `firebase.identities["google.com"]` の識別子をOAuth stateへ保存する。Firebase UIDとGoogleのsubjectは別の値として扱う。
- Calendar認証後のID tokenをGoogle SDKで検証し、subjectがログイン中のGoogleアカウントと一致する場合だけトークンを保存する。別アカウントを選んだ場合は拒否する。
- `login_hint` とアカウント選択画面を使い、本人のアカウントを選びやすくする。照合はヒントのメール文字列ではなく、検証済みsubjectで行う。
- ChatはGoogleの署名・Audience・送信元を検証し、送信者のGoogle IDから本人のFirebase UIDへ対応付ける。メッセージの再送記録も所有者を確認する。
- DM以外のイベントでは課題を登録しない。`spaceType=DIRECT_MESSAGE` と、旧形式の `type=DM` に対応する。明示された `spaceType` を優先し、種類不明の会話も受け付けない。
- Webのアカウント変更時に前の課題・プロフィール・ダイアログを消す。前アカウントの遅れたAPI応答が新アカウントの表示へ戻らないようにする。
- 拡張もアカウント変更で予定・通知・計測中の未送信分を解除する。古い接続の遅れた応答を採用しない。同じChromeプロフィールでは最後に接続したアカウントを使う。人ごとにChromeプロフィールを分ける運用を案内する。
- 公開の[データ取り扱いページ](../web/public/privacy.html)を用意した。ログアウトはサーバーデータ削除ではない。現時点の削除は本人確認を伴う運営への依頼で扱い、自動退会機能があるとは案内しない。

必要なGoogleアカウント識別子は検証済みID tokenから得るため、実行サービスアカウントへのFirebaseユーザー管理権限の追加は不要。

## Google側で確認した現状

- OAuthは外部・本番環境。テストユーザー一覧だけに限定する設定ではない。
- 宣言されているCalendarの `calendar.app.created`、`userinfo.email`、`openid` はコンソールの非機密スコープ欄にあり、機密・制限付きスコープ欄は空だった。
- Chatは個人アカウントで設定されており、「個人使用のアプリのみを作成できます。公開設定などは自動的に無効になります」と表示される。許可ユーザーの欄は個人1件・編集不可。一般配布完了ではない。
- Chatの「スペースとグループの会話に参加する」はオフ。既存の個人用DMを維持している。

Chat一般公開を完了するには、公開作業を行うWorkspaceアカウントと運営管理主体を決め、Marketplaceの設定・掲載情報・必要な審査を進める。現段階で、別アカウントがChatアプリをインストールできると保証しない。組織・Googleグループ・IAMの変更は、対象が確定してから行う。

公式資料：

- [Chat APIの構成とWorkspace要件](https://developers.google.com/workspace/chat/configure-chat-api)
- [Chatのテスト対象と一般配布](https://developers.google.com/workspace/chat/test-interactive-features)
- [Marketplaceの公開設定](https://developers.google.com/workspace/marketplace/enable-configure-sdk)
- [Calendarスコープ](https://developers.google.com/workspace/calendar/api/auth)

## 回帰確認

- Node：古い利用者の応答を保留してアカウントを切り替え、遅れた応答が表示・拡張へ戻らないこと。ログアウト後の課題・通知解除。
- Go：Google subjectの照合、Firebase UIDとの区別、デモStoreの2ユーザーfixtureで別ユーザーの課題の読み取り・開始・完了・予定確認の拒否、同じ課題IDを使う2ユーザーの報酬分離、DMと共有会話の判定。
- 既存のビルド・型チェック・時刻テスト・Go race・vetも実行する。実アカウントの同意と一般配布の完了は、自動試験と分けて記録する。
