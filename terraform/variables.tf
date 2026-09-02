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
  description = <<-EOT
    Default Container Registry ID to watch. Used by every entry in
    image_container_map that does not name a registry of its own.
  EOT
}

variable "image_container_map" {
  type = map(object({
    image        = string
    container_id = string
    registry_id  = optional(string)
    tag          = optional(string)
    secret_ids   = optional(list(string), [])
  }))
  description = <<-EOT
    Deploy channels, keyed by an arbitrary label that names the channel.
    One trigger is created per entry, and the label is what distinguishes
    them, so two channels may watch the same image as long as their labels
    differ.

      image        Image repository name, without the registry_id prefix.
      container_id Serverless Container to redeploy (yc serverless container list).
      registry_id  Registry holding the image. Defaults to var.registry_id,
                   which is what lets one stack serve several registries.
      tag          Only this tag fires the trigger, and only this tag routes
                   to this container. Omitted means any tag.
      secret_ids   Lockbox secrets the container's revision mounts. The
                   function is granted lockbox.payloadViewer on each, without
                   which DeployRevision refuses the revision with 403. Leave
                   empty for a container that mounts no secrets.

    Example:
      {
        myapp = {
          image        = "myapp"
          container_id = "bba..."
        }
        otherapp-test = {
          image        = "otherapp"
          registry_id  = "crp..."
          tag          = "test"
          container_id = "bbb..."
          secret_ids   = ["e6q..."]
        }
      }
  EOT

  validation {
    condition     = alltrue([for v in var.image_container_map : v.image != "" && v.container_id != ""])
    error_message = "Every entry needs a non-empty image and container_id."
  }
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
