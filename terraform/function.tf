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
    # JSON map: { "registry-id/image[:tag]": "container-id", ... }
    # Built from var.image_container_map so adding a channel only requires
    # updating terraform.tfvars and re-applying.
    #
    # Keys carry the registry_id prefix to match the repository_name field in
    # Container Registry trigger events (e.g. "crp.../urlshortener"), and carry
    # the tag when the channel pins one, which is how two channels cut from the
    # same image reach different containers. Terraform fails on a duplicate
    # key, so two channels cannot silently claim the same image and tag.
    IMAGE_CONTAINER_MAP = jsonencode({
      for k, v in local.channels :
      (v.tag == null
        ? "${v.registry_id}/${v.image}"
      : "${v.registry_id}/${v.image}:${v.tag}") => v.container_id
    })
  }
}
