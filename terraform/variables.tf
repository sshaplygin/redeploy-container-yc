variable "folder_id" {
  type        = string
  description = "Yandex Cloud folder ID where all resources will be created."
}

variable "zone" {
  type        = string
  default     = "ru-central1-a"
  description = "Availability zone."
}

variable "function_name" {
  type        = string
  default     = "registry-deploy"
  description = "Name of the Cloud Function."
}

variable "registry_id" {
  type        = string
  description = "Container Registry ID to watch for image pushes."
}

variable "image_container_map" {
  type        = map(string)
  description = <<-EOT
    Map of image repository name → Serverless Container ID.
    One trigger per entry is created, scoped to that image name.

    Example:
      {
        "urlshortener" = "bba..."
        "otherapp"     = "bbb..."
      }
  EOT
}

variable "function_memory" {
  type        = number
  default     = 128
  description = "Memory allocated to the function in MB."
}

variable "function_timeout" {
  type        = number
  default     = 30
  description = "Function execution timeout in seconds."
}
