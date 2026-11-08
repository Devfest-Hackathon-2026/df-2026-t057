# Secret の器だけを管理する。値は state に入れないよう、Terraform の外で `gcloud secrets versions add` で入れる。
# 利用者ごとの Calendar トークン（dopagaki-calendar-<uid>）は API が作って消すので、ここには書かない。

resource "google_secret_manager_secret" "calendar_oauth" {
  secret_id = "dopagaki-calendar-oauth"

  replication {
    auto {}
  }
}
