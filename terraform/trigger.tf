# One trigger per entry in var.image_container_map.
# Each trigger watches the same registry but filters on its own image name,
# so only pushes to that repository invoke the function.

resource "yandex_function_trigger" "registry_push" {
  for_each = var.image_container_map

  name        = "${var.function_name}-${each.key}"
  description = "Fires on new tag push to image '${each.key}' and deploys container ${each.value}."
  folder_id   = var.folder_id

  container_registry {
    registry_id = var.registry_id
    image_name  = each.key

    # Fire when a new tag is pushed (e.g. pr-42, latest, sha-abc123).
    create_image_tag {}
  }

  function {
    id                 = yandex_function.deploy.id
    service_account_id = yandex_iam_service_account.trigger_sa.id
  }
}
