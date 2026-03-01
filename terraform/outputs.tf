output "function_id" {
  description = "Cloud Function ID."
  value       = yandex_function.deploy.id
}

output "function_name" {
  description = "Cloud Function name."
  value       = yandex_function.deploy.name
}

output "trigger_ids" {
  description = "Map of image name → trigger ID."
  value       = { for k, v in yandex_function_trigger.registry_push : k => v.id }
}

output "function_sa_id" {
  description = "Service account ID used by the function at runtime."
  value       = yandex_iam_service_account.function_sa.id
}

output "trigger_sa_id" {
  description = "Service account ID used by the registry trigger to invoke the function."
  value       = yandex_iam_service_account.trigger_sa.id
}
