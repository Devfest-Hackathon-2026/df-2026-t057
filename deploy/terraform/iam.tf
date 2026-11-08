resource "google_service_account" "runtime" {
  account_id   = "dopagaki-runtime"
  display_name = "DOPAGAKI runtime"
}

resource "google_service_account" "invoker" {
  account_id   = "dopagaki-invoker"
  display_name = "DOPAGAKI Cloud Tasks / Scheduler invoker"
}

resource "google_service_account" "deploy" {
  account_id   = "dopagaki-deploy"
  display_name = "DOPAGAKI GitHub deployer"
}

# Cloud Build で手動配信するときに使う。CD（GitHub Actions）は使わない。
resource "google_service_account" "build" {
  account_id   = "dopagaki-build"
  display_name = "DOPAGAKI build"
}

# 実行 SA の権限。定義済みロールは使わない権限まで多く含むので、SDK が呼ぶ権限だけのカスタムロールにした
# （engineering/security-hardening-2026-11-07.md）。

resource "google_project_iam_custom_role" "gemini_inference" {
  role_id     = "dopagakiGeminiInference"
  title       = "DOPAGAKI Gemini inference"
  description = "Invoke Gemini without model, endpoint or training administration."
  permissions = ["aiplatform.endpoints.predict"]
}

resource "google_project_iam_custom_role" "data_access" {
  role_id     = "dopagakiDataAccess"
  title       = "DOPAGAKI application data"
  description = "Read and transact on existing application data without console or database administration."
  permissions = [
    "datastore.databases.get",
    "datastore.entities.create",
    "datastore.entities.delete",
    "datastore.entities.get",
    "datastore.entities.list",
    "datastore.entities.update",
  ]
}

resource "google_project_iam_custom_role" "firebase_session_verification" {
  role_id     = "dopagakiFirebaseSessionVerification"
  title       = "DOPAGAKI Firebase session verification"
  description = "Read Firebase users to check revoked or deleted authentication sessions"
  permissions = ["firebaseauth.users.get"]
}

resource "google_project_iam_custom_role" "secret_creator" {
  role_id     = "dopagakiSecretCreator"
  title       = "DOPAGAKI calendar secret creator"
  description = "Create application-owned Calendar token secrets; access is granted separately by prefix."
  permissions = ["secretmanager.secrets.create"]
}

# 利用者のトークンを保存するたびに古い版を破棄し、連携の解除で Secret ごと消す（apps/api/internal/adapter/gcal/secrets.go）。
resource "google_project_iam_custom_role" "calendar_token_lifecycle" {
  role_id     = "dopagakiCalendarTokenLifecycle"
  title       = "DOPAGAKI user calendar token lifecycle"
  description = "Rotate and delete per-user Google Calendar OAuth tokens (dopagaki-calendar-* secrets) without reading or writing other secrets."
  permissions = [
    "secretmanager.secrets.delete",
    "secretmanager.versions.destroy",
    "secretmanager.versions.list",
  ]
}

locals {
  runtime_member = "serviceAccount:${google_service_account.runtime.email}"
  secrets_prefix = "projects/${var.project_number}/secrets"

  # 作成前の Secret の名前には条件を付けられないので、作成（secret_creator）は条件なしで付ける。
  runtime_roles = {
    gemini_inference              = google_project_iam_custom_role.gemini_inference.name
    data_access                   = google_project_iam_custom_role.data_access.name
    firebase_session_verification = google_project_iam_custom_role.firebase_session_verification.name
    secret_creator                = google_project_iam_custom_role.secret_creator.name
  }

  # 書き込みは利用者ごとの Calendar トークンだけ。静的な OAuth client secret は書き換えさせない。
  calendar_token_write = {
    title      = "dopagaki-user-calendar-token-write"
    expression = "resource.name.startsWith('${local.secrets_prefix}/dopagaki-calendar-') && resource.name != '${local.secrets_prefix}/dopagaki-calendar-oauth' && !resource.name.startsWith('${local.secrets_prefix}/dopagaki-calendar-oauth/')"
  }
}

resource "google_project_iam_member" "runtime" {
  for_each = local.runtime_roles

  project = var.project_id
  role    = each.value
  member  = local.runtime_member
}

resource "google_project_iam_member" "runtime_secret_access" {
  project = var.project_id
  role    = "roles/secretmanager.secretAccessor"
  member  = local.runtime_member

  condition {
    title      = "dopagaki-runtime-secret-access"
    expression = "resource.name.startsWith('${local.secrets_prefix}/dopagaki-calendar-') || resource.name == '${local.secrets_prefix}/dopagaki-scheduler' || resource.name.startsWith('${local.secrets_prefix}/dopagaki-scheduler/versions/')"
  }
}

resource "google_project_iam_member" "runtime_secret_version_adder" {
  project = var.project_id
  role    = "roles/secretmanager.secretVersionAdder"
  member  = local.runtime_member

  condition {
    title      = local.calendar_token_write.title
    expression = local.calendar_token_write.expression
  }
}

resource "google_project_iam_member" "runtime_calendar_token_lifecycle" {
  project = var.project_id
  role    = google_project_iam_custom_role.calendar_token_lifecycle.name
  member  = local.runtime_member

  condition {
    title      = local.calendar_token_write.title
    expression = local.calendar_token_write.expression
  }
}

resource "google_project_iam_member" "build" {
  project = var.project_id
  role    = "roles/run.builder"
  member  = "serviceAccount:${google_service_account.build.email}"
}

# 実行 SA が、Cloud Tasks のタスクに invoker の OIDC トークンを付けられるようにする。
resource "google_service_account_iam_member" "runtime_acts_as_invoker" {
  service_account_id = google_service_account.invoker.name
  role               = "roles/iam.serviceAccountUser"
  member             = local.runtime_member
}

# CD が、実行 SA で動くリビジョンを配信できるようにする。
resource "google_service_account_iam_member" "deploy_acts_as_runtime" {
  service_account_id = google_service_account.runtime.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deploy.email}"
}
