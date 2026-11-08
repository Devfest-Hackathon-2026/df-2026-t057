# API が使う Google Cloud の API。Firebase の設定やほかの用途で有効にしたものは、ここでは扱わない。
locals {
  services = [
    "aiplatform.googleapis.com",
    "artifactregistry.googleapis.com",
    "calendar-json.googleapis.com",
    "cloudscheduler.googleapis.com",
    "cloudtasks.googleapis.com",
    "firebaserules.googleapis.com",
    "firestore.googleapis.com",
    "iam.googleapis.com",
    "iamcredentials.googleapis.com",
    "identitytoolkit.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "sts.googleapis.com",
  ]
}

resource "google_project_service" "enabled" {
  for_each = toset(local.services)

  service = each.value
  # Terraform の管理から外しても、API は止めない。止めるとほかの設定まで消えることがある。
  deletion_policy = "ABANDON"
}
