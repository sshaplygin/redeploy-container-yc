# One trigger per channel in var.image_container_map.
# Each trigger filters on its own registry, image and (optionally) tag, so only
# the pushes that belong to that channel invoke the function.

resource "yandex_function_trigger" "registry_push" {
  for_each = local.channels

  name        = "${var.function_name}-${each.key}"
  description = "Fires on a push of image '${each.value.image}'${each.value.tag == null ? "" : " tagged '${each.value.tag}'"} and deploys container ${each.value.container_id}."
  folder_id   = var.folder_id

  container_registry {
    registry_id  = each.value.registry_id
    image_name   = "${each.value.registry_id}/${each.value.image}"
    tag          = each.value.tag
    batch_cutoff = "1"
    batch_size   = "1"

    # Fire when a tag is pushed. This covers both flows that matter: a fresh
    # `docker push` of a newly built image, and moving an existing tag onto an
    # image already in the registry (how a build is promoted to another
    # channel). Verified against the running urlshortener deployment, where a
    # plain push of a new image produced a revision one second later.
    create_image_tag = true
  }

  function {
    id                 = yandex_function.deploy.id
    service_account_id = yandex_iam_service_account.trigger_sa.id
  }
}
