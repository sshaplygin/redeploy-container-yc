# ── Function runtime service account ─────────────────────────────────────────
# Used by the function itself to call the Serverless Containers API.

resource "yandex_iam_service_account" "function_sa" {
  name        = "${var.function_name}-sa"
  description = "Runtime SA for the ${var.function_name} Cloud Function."
  folder_id   = var.folder_id
}

# Allow the function SA to deploy new container revisions.
resource "yandex_resourcemanager_folder_iam_member" "function_containers_editor" {
  folder_id = var.folder_id
  role      = "serverless-containers.editor"
  member    = "serviceAccount:${yandex_iam_service_account.function_sa.id}"
}

# ── Trigger invoker service account ──────────────────────────────────────────
# Used by the Container Registry trigger to invoke the function.

resource "yandex_iam_service_account" "trigger_sa" {
  name        = "${var.function_name}-trigger-sa"
  description = "SA used by the registry trigger to invoke ${var.function_name}."
  folder_id   = var.folder_id
}

# Allow the trigger SA to invoke the function.
resource "yandex_resourcemanager_folder_iam_member" "trigger_function_invoker" {
  folder_id = var.folder_id
  role      = "serverless.functions.invoker"
  member    = "serviceAccount:${yandex_iam_service_account.trigger_sa.id}"
}
