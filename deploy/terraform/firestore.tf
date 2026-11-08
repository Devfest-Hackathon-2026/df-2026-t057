resource "google_firestore_database" "default" {
  name        = "(default)"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"
  # 本番データがあるので、Terraform からは消せないようにする。
  deletion_policy = "PREVENT"
}

# 期限（expiresAt）を過ぎたドキュメントを Firestore が自動で消す。
resource "google_firestore_field" "ttl" {
  for_each = toset(["idempotency", "outbox", "oauthStates", "accountJobs"])

  database   = google_firestore_database.default.name
  collection = each.value
  field      = "expiresAt"

  ttl_config {}
}

# 記録した応答の本文は大きく、インデックスの上限（1項目 7.5 KiB）に当たって書き込みが失敗するので索引しない。
resource "google_firestore_field" "idempotency_body" {
  database   = google_firestore_database.default.name
  collection = "idempotency"
  field      = "body"

  index_config {}
}

# クライアントからの直接アクセスは全部拒否する。読み書きは API の実行 SA だけが行う。
resource "google_firebaserules_ruleset" "firestore" {
  source {
    files {
      name    = "firestore.rules"
      content = file("${path.module}/../firestore.rules")
    }
  }
}

resource "google_firebaserules_release" "firestore" {
  name         = "cloud.firestore"
  ruleset_name = google_firebaserules_ruleset.firestore.name
}
