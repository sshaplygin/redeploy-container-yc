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

# Allow the function SA to assign service accounts when deploying revisions.
# Required because the copied revision config carries a serviceAccountId and
# Yandex Cloud validates that the caller can "use" that service account.
resource "yandex_resourcemanager_folder_iam_member" "function_sa_user" {
  folder_id = var.folder_id
  role      = "iam.serviceAccounts.user"
  member    = "serviceAccount:${yandex_iam_service_account.function_sa.id}"
}

# Allow the function SA to attach VPC networks when deploying revisions.
# Required because the copied revision config may carry a connectivity/VPC
# network reference, and Yandex Cloud validates vpc.user on the caller.
resource "yandex_resourcemanager_folder_iam_member" "function_vpc_user" {
  folder_id = var.folder_id
  role      = "vpc.user"
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

# ── Lockbox access for the secrets the target containers mount ───────────────
# The function copies the current revision's `secrets` block verbatim into the
# revision it deploys, and Yandex Cloud will not let a caller attach a secret it
# cannot itself read: DeployRevision answers 403 Permission denied, after the
# event has been parsed and the container resolved, so the trigger looks healthy
# and only the function's log shows the failure.
#
# The role is granted per secret named in a channel's secret_ids rather than on
# the folder, so the function can read exactly the secrets belonging to the
# containers it deploys and nothing else.
resource "yandex_lockbox_secret_iam_member" "function_payload_viewer" {
  for_each = local.function_secret_ids

  secret_id = each.value
  role      = "lockbox.payloadViewer"
  member    = "serviceAccount:${yandex_iam_service_account.function_sa.id}"
}

# Deploying a revision that carries Lockbox secrets needs more than
# serverless-containers.editor: Yandex Cloud additionally checks
# functions.editor on the caller. This is the documented behaviour of the
# platform, not a guess — the README of yc-actions/yc-sls-container-deploy
# says of secrets that "serverless-containers.editor [is] missing some
# permissions, so you have to use this one additionally". Established here
# empirically as well: with the secrets block stripped from the revision the
# function deploys fine on editor alone, and with secrets present the deploy
# is 403 regardless of any lockbox.* grant on the secret or the folder.
resource "yandex_resourcemanager_folder_iam_member" "function_functions_editor" {
  folder_id = var.folder_id
  role      = "functions.editor"
  member    = "serviceAccount:${yandex_iam_service_account.function_sa.id}"
}
