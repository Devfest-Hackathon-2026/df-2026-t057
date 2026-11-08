terraform {
  required_version = "1.17.1"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "8.10.0"
    }
  }

  # バケットは Terraform の外で一度だけ作る（README の「最初の準備」）。backend には変数を使えない。
  backend "gcs" {
    bucket = "devfest-2026-513816-tfstate"
    prefix = "dopagaki"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  # 個人のアカウントで実行すると、Firebase Rules などの API は課金先のプロジェクトの指定を求める。
  user_project_override = true
  billing_project       = var.project_id
}
