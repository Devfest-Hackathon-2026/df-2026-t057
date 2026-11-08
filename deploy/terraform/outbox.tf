# Outbox のジョブを API へ届ける Cloud Tasks のキューと、毎日の保守を呼ぶ Cloud Scheduler（AGENTS.md「外部サービスとの整合」）。

resource "google_cloud_tasks_queue" "outbox" {
  name     = "dopagaki-outbox"
  location = var.region

  rate_limits {
    max_dispatches_per_second = 5
    max_concurrent_dispatches = 10
  }

  # API は8回失敗したジョブを failed にして 2xx を返す（platform/outbox の MaxAttempts）。キューの上限はそれより大きくする。
  retry_config {
    max_attempts  = 10
    min_backoff   = "10s"
    max_backoff   = "3600s"
    max_doublings = 8
  }
}

resource "google_cloud_tasks_queue_iam_member" "runtime_enqueuer" {
  name     = google_cloud_tasks_queue.outbox.name
  location = google_cloud_tasks_queue.outbox.location
  role     = "roles/cloudtasks.enqueuer"
  member   = local.runtime_member
}

# 期限が近い Calendar の変更通知のチャンネルを更新し、実行されないまま残ったジョブを登録し直す。
resource "google_cloud_scheduler_job" "calendar_renew" {
  name             = "dopagaki-calendar-renew"
  region           = var.region
  schedule         = "0 4 * * *"
  time_zone        = "Etc/UTC"
  attempt_deadline = "300s"

  http_target {
    uri         = "${local.app_url}/api/internal/calendar/renew"
    http_method = "POST"

    oidc_token {
      service_account_email = google_service_account.invoker.email
      audience              = local.app_url
    }
  }
}
