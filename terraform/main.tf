terraform {
  required_providers {
    yandex = {
      source  = "yandex-cloud/yandex"
      version = "~> 0.120"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.4"
    }
  }

  # Uncomment to store state in Yandex Object Storage:
  # backend "s3" {
  #   endpoint                    = "storage.yandexcloud.net"
  #   bucket                      = "<your-bucket>"
  #   key                         = "registry-deploy/terraform.tfstate"
  #   region                      = "ru-central1"
  #   skip_region_validation      = true
  #   skip_credentials_validation = true
  # }
}

provider "yandex" {
  folder_id = var.folder_id
  zone      = var.zone
}

# Each channel's effective registry, resolved once so trigger.tf and function.tf
# cannot disagree about which registry an image lives in.
locals {
  channels = {
    for k, v in var.image_container_map : k => merge(v, {
      registry_id = coalesce(v.registry_id, var.registry_id)
    })
  }
}

# Zip the Go source so Terraform can upload it as function content.
data "archive_file" "function_zip" {
  type        = "zip"
  source_dir  = "${path.module}/../function"
  output_path = "${path.module}/function.zip"
  excludes    = ["*.md"]
}
