variable "project_id" {
  type = string
}

variable "project_number" {
  type = string
}

variable "region" {
  type = string
}

variable "github_repository" {
  type        = string
  description = "owner/name。配信を許すワークフローの指定に使う"
}

variable "github_repository_id" {
  type        = string
  description = "名前の付け替えや fork で配信 SA を使われないよう、数値の ID でも絞る"
}

variable "github_owner_id" {
  type = string
}

variable "firebase_web_config" {
  type        = string
  description = "Firebase の Web アプリの設定（JSON）。公開してよい値だけを含む"
}

variable "google_oauth_client_id" {
  type = string
}

variable "google_oauth_client_secret_version" {
  type = string
}

variable "extension_ids" {
  type        = list(string)
  description = "状態を変える要求を受け付ける Chrome 拡張の ID"
}
