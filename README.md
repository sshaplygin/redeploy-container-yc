# redeploy-container-yc

Automatically redeploys a Yandex Cloud Serverless Container whenever a new image tag is pushed to Container Registry.

## How it works

```
Container Registry push
        │
        ▼
  YC Function Trigger
  (one per image repo)
        │
        ▼
  Cloud Function (Go)
        │
   1. Resolve container ID from IMAGE_CONTAINER_MAP
   2. Fetch active revision config
   3. Deploy new revision with updated imageUrl
        │
        ▼
  Serverless Container
  running the new image
```

## Repository layout

```
.
├── function/          # Go source for the Cloud Function
│   ├── main.go        # Handler entrypoint + helper functions
│   ├── go.mod
│   └── go.sum
└── terraform/         # Infrastructure as code
    ├── main.tf        # Providers, archive data source
    ├── function.tf    # yandex_function resource
    ├── trigger.tf     # yandex_function_trigger (one per image)
    ├── iam.tf         # Service accounts and IAM bindings
    ├── variables.tf   # Input variable definitions
    ├── outputs.tf     # Output values
    └── terraform.tfvars.example
```

## Prerequisites

- [Yandex Cloud CLI (`yc`)](https://yandex.cloud/docs/cli/)
- [Terraform >= 1.3](https://developer.hashicorp.com/terraform/install)
- [Go 1.23+](https://go.dev/dl/) (for local builds / tests only)
- A Yandex Cloud folder with billing enabled
- An existing Container Registry

## Deployment

### 1. Configure variables

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars`:

| Variable | Description |
|---|---|
| `folder_id` | Yandex Cloud folder ID (`yc resource-manager folder list`) |
| `registry_id` | Container Registry ID (`yc container registry list`) |
| `image_container_map` | Deploy channels: label → `{image, container_id, registry_id?, tag?}` |
| `function_name` | Cloud Function name (default: `registry-deploy`) |
| `function_memory` | Memory in MB (default: `128`) |
| `function_timeout` | Timeout in seconds (default: `30`) |

Example `image_container_map`:

```hcl
image_container_map = {
  # Simplest case: default registry, any tag, one container.
  myapp = {
    image        = "myapp"
    container_id = "bba..."
  }

  # Another registry, and one image feeding two containers by tag.
  otherapp-test = {
    image        = "otherapp"
    registry_id  = "crp..."
    tag          = "test"
    container_id = "bbb..."
  }
  otherapp-prod = {
    image        = "otherapp"
    registry_id  = "crp..."
    tag          = "prod"
    container_id = "bbc..."
  }
}
```

> The key is a label for the channel, not the image name — that is what lets
> `otherapp-test` and `otherapp-prod` watch one image and reach two containers.
> `registry_id` is per entry and defaults to the top-level `registry_id`, so one
> stack can serve several registries. `tag` narrows both the trigger and the
> routing; omit it and any tag on that image fires the channel.
>
> Terraform builds the `IMAGE_CONTAINER_MAP` env var from these entries,
> prefixing the registry so keys match the `repository_name` field in trigger
> events (`crp.../myapp`), and appending `:tag` where a channel pins one.

### 2. Apply

```bash
export YC_SERVICE_ACCOUNT_KEY_FILE=$(cat key.json)

terraform init
terraform plan
terraform apply
```

Terraform will create:

- One Cloud Function (`registry-deploy`) with the Go handler zipped and uploaded
- One trigger per entry in `image_container_map`, each scoped to its repository name
- Two service accounts with minimal IAM roles:
  - **function-sa** — `serverless-containers.editor`, `iam.serviceAccounts.user`, `vpc.user`
  - **trigger-sa** — `serverless.functions.invoker` (invokes the function)

### 3. Adding a new container

Add an entry to `image_container_map` in `terraform.tfvars` and re-run `terraform apply`. A new trigger is created automatically; no code changes needed.

### 4. Promoting a build between channels

Two channels on one image are a promotion path: CI pushes the `test` tag on
merge, and production is deployed by moving `prod` onto a build that has
already proven itself in test. Moving the tag is the whole deploy — no call to
the Serverless Containers API, so the promoting workflow needs registry
credentials only:

```bash
IMAGE=cr.yandex/<registry-id>/otherapp
docker buildx imagetools create --tag "$IMAGE:prod" "$IMAGE:pr-42"
```

Rolling back is the same command with an older source tag.

## Function environment variable

| Variable              | Format                                        | Description                                              |
|-----------------------|-----------------------------------------------|----------------------------------------------------------|
| `IMAGE_CONTAINER_MAP` | JSON `{"registry_id/repo[:tag]": "container-id"}` | Auto-set by Terraform from `image_container_map` variable|

The function prefers a key that pins the tag (`repo:tag`) over one that matches
the repository alone (`repo`), so a bare `repo` key still matches any tag and
maps written before tag routing keep working unchanged.

## IAM roles required

### Terraform deployer service account

The service account used to run `terraform apply` (e.g. `registry-deploy-sa`) needs:

| Role | Purpose |
|---|---|
| `container-registry.images.puller` | Pull images from Container Registry |
| `functions.editor` | Create and manage Cloud Functions |
| `iam.serviceAccounts.admin` | Create and bind runtime service accounts |
| `resource-manager.admin` | Manage folder-level resources |
| `serverless-containers.editor` | Create and manage Serverless Containers |
| `serverless.functions.invoker` | Invoke Cloud Functions |

### Runtime service accounts (created by Terraform)

| Role | Assigned to | Purpose |
|---|---|---|
| `serverless-containers.editor` | `<function_name>-sa` | Deploy new container revisions at runtime |
| `iam.serviceAccounts.user` | `<function_name>-sa` | Assign the container's service account when deploying a revision |
| `vpc.user` | `<function_name>-sa` | Attach a VPC network when the revision config carries connectivity settings |
| `serverless.functions.invoker` | `<function_name>-trigger-sa` | Invoke the Cloud Function from the registry trigger |

## Local development

```bash
cd function
go vet ./...
```

> The package has no `main()` — Yandex Cloud Functions use `Handler` as the
> entrypoint (`main.Handler`). That is also why `go build ./...` fails here
> with "function main is undeclared in the main package"; `go vet` type-checks
> the package and is the build check to run.

## Optional: remote Terraform state

Uncomment the `backend "s3"` block in [terraform/main.tf](terraform/main.tf) and set your Object Storage bucket name to store state remotely.
