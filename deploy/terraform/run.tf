locals {
  # Cloud Run の決まった形の URL。サービス自身の環境変数に入れるので、リソースの属性からは引けない（循環する）。
  app_url = "https://dopagaki-${var.project_number}.${var.region}.run.app"
}

resource "google_artifact_registry_repository" "app" {
  repository_id = "dopagaki"
  location      = var.region
  format        = "DOCKER"
  description   = "DOPAGAKI application images"
}

resource "google_artifact_registry_repository_iam_member" "deploy_writer" {
  location   = google_artifact_registry_repository.app.location
  repository = google_artifact_registry_repository.app.name
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.deploy.email}"
}

resource "google_cloud_run_v2_service" "app" {
  name     = "dopagaki"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  scaling {
    max_instance_count = 20
  }

  template {
    service_account                  = google_service_account.runtime.email
    timeout                          = "60s"
    max_instance_request_concurrency = 40

    scaling {
      max_instance_count = 2
    }

    containers {
      # イメージは CD（deploy/cloud-run.sh）が入れ替える。Terraform は lifecycle で無視する。
      image = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.app.repository_id}/app"

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      env {
        name  = "APP_MODE"
        value = "live"
      }
      env {
        name  = "GOOGLE_CLOUD_PROJECT"
        value = var.project_id
      }
      env {
        name  = "GOOGLE_CLOUD_LOCATION"
        value = "global"
      }
      env {
        name  = "GEMINI_MODEL"
        value = "gemini-3.8-flash"
      }
      env {
        name  = "APP_URL"
        value = local.app_url
      }
      env {
        name  = "FIREBASE_WEB_CONFIG"
        value = var.firebase_web_config
      }
      env {
        name  = "GOOGLE_OAUTH_CLIENT_ID"
        value = var.google_oauth_client_id
      }
      env {
        name = "GOOGLE_OAUTH_CLIENT_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.calendar_oauth.secret_id
            version = var.google_oauth_client_secret_version
          }
        }
      }
      env {
        name  = "AI_USER_PER_MINUTE"
        value = "5"
      }
      env {
        name  = "AI_USER_PER_DAY"
        value = "30"
      }
      env {
        name  = "AI_GLOBAL_PER_MINUTE"
        value = "30"
      }
      env {
        name  = "AI_GLOBAL_PER_DAY"
        value = "300"
      }
      env {
        name  = "CLOUD_TASKS_QUEUE"
        value = google_cloud_tasks_queue.outbox.id
      }
      env {
        name  = "CLOUD_TASKS_INVOKER"
        value = google_service_account.invoker.email
      }
      env {
        name  = "SCHEDULER_INVOKER"
        value = google_service_account.invoker.email
      }
      env {
        name  = "OIDC_AUDIENCE"
        value = local.app_url
      }
      env {
        name  = "EXTENSION_IDS"
        value = join(",", var.extension_ids)
      }
    }
  }

  # template.revision は無視しない。無視すると、環境変数を変えるときに CD が付けた古いリビジョン名のまま送り、
  # 同じ名前のリビジョンがあるとして拒否される。そのため CD のあとの plan には revision の差分が出る。
  # トラフィックは CD が確認済みのリビジョンに固定するので、apply で作ったリビジョンには流れない（README）。
  lifecycle {
    ignore_changes = [
      # CD が配信のたびに変える。
      client,
      client_version,
      traffic,
      template[0].containers[0].image,
    ]
  }
}

# 個人データは API が Firebase の ID トークンで守る。Cloud Run の入口は誰でも呼べるようにしておく。
resource "google_cloud_run_v2_service_iam_member" "public" {
  name     = google_cloud_run_v2_service.app.name
  location = google_cloud_run_v2_service.app.location
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_service_iam_member" "invoker" {
  name     = google_cloud_run_v2_service.app.name
  location = google_cloud_run_v2_service.app.location
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.invoker.email}"
}

resource "google_cloud_run_v2_service_iam_member" "deploy_developer" {
  name     = google_cloud_run_v2_service.app.name
  location = google_cloud_run_v2_service.app.location
  role     = "roles/run.developer"
  member   = "serviceAccount:${google_service_account.deploy.email}"
}
