resource "yandex_function" "deploy" {
  name        = var.function_name
  description = "Deploys a new Serverless Container revision when an image is pushed to the registry."
  folder_id   = var.folder_id

  # user_hash forces a re-deploy whenever the zip content changes.
  user_hash = data.archive_file.function_zip.output_sha256

  runtime           = "golang123"
  entrypoint        = "main.Handler"
  memory            = var.function_memory
  execution_timeout = tostring(var.function_timeout)

  service_account_id = yandex_iam_service_account.function_sa.id

  content {
    zip_filename = data.archive_file.function_zip.output_path
  }

  environment = {
    # JSON map: { "image-repo-name": "container-id", ... }
    # Built from var.image_container_map so adding a new project only
    # requires updating terraform.tfvars and re-applying.
    # Keys are prefixed with registry_id to match the repository_name field
    # returned by the Container Registry trigger event (e.g. "crp.../urlshortener").
    IMAGE_CONTAINER_MAP = jsonencode({
      for k, v in var.image_container_map : "${var.registry_id}/${k}" => v
    })
  }
}
